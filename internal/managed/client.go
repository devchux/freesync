package managed

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/devchux/freesync/internal/service"
	"github.com/devchux/freesync/internal/storage"
)

type Client struct {
	endpoint string
	token    string
	writable bool
}

func NewClient(endpoint, drive, token string) (*Client, error) {
	base, err := serverURL(endpoint)
	if err != nil {
		return nil, err
	}
	if !validPath(drive) || strings.Contains(drive, "/") || token == "" {
		return nil, fmt.Errorf("managed drive and credential are required")
	}
	return &Client{endpoint: base + "/drives/" + url.PathEscape(drive), token: token}, nil
}

func serverURL(endpoint string) (string, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("invalid managed server URL")
	}
	ip := net.ParseIP(u.Hostname())
	if u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "localhost" || ip.IsLoopback())) {
		return "", fmt.Errorf("managed server URL requires HTTPS outside loopback")
	}
	return strings.TrimRight(u.String(), "/"), nil
}

var ErrSessionExpired = errors.New("managed session expired or is invalid; sign in again")

func SignUp(ctx context.Context, endpoint, fullName, email, password string) error {
	response, err := sendCredentials(ctx, endpoint, "signup", credentials{FullName: fullName, Email: email, Password: password})
	if err != nil {
		return err
	}
	return response.Body.Close()
}

func Login(ctx context.Context, endpoint, email, password string) (Account, string, error) {
	response, err := sendCredentials(ctx, endpoint, "login", credentials{Email: email, Password: password})
	if err != nil {
		return Account{}, "", err
	}
	defer response.Body.Close()
	var account Account
	if err := json.NewDecoder(response.Body).Decode(&account); err != nil {
		return Account{}, "", err
	}
	for _, cookie := range response.Cookies() {
		if (cookie.Name == sessionCookie || cookie.Name == secureSessionCookie) && cookie.Value != "" {
			return account, cookie.Value, nil
		}
	}
	return Account{}, "", errors.New("managed login returned no session")
}

func sendCredentials(ctx context.Context, endpoint, action string, input credentials) (*http.Response, error) {
	base, err := serverURL(endpoint)
	if err != nil {
		return nil, err
	}
	payload, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/auth/"+action, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	response, err := transport.Do(req)
	if err != nil {
		return nil, err
	}
	if response.StatusCode == http.StatusCreated || response.StatusCode == http.StatusOK {
		return response, nil
	}
	response.Body.Close()
	switch response.StatusCode {
	case http.StatusUnauthorized:
		return nil, errors.New("invalid email or password")
	case http.StatusConflict:
		return nil, errors.New("an account already exists for this email")
	case http.StatusBadRequest:
		return nil, errors.New("enter a valid email, full name for signup, and a password of 12–72 bytes")
	default:
		return nil, fmt.Errorf("managed %s: HTTP %d", action, response.StatusCode)
	}
}

var transport = http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

func (c *Client) Request(ctx context.Context, method, action string, params url.Values, body io.Reader) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.endpoint+"/"+action+"?"+params.Encode(), body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	res, err := transport.Do(req)
	if err != nil {
		return nil, err
	}
	if res.StatusCode >= 200 && res.StatusCode < 300 {
		return res.Body, nil
	}
	res.Body.Close()
	switch res.StatusCode {
	case http.StatusUnauthorized:
		return nil, ErrSessionExpired
	case http.StatusForbidden:
		return nil, fs.ErrPermission
	case http.StatusNotFound:
		return nil, fs.ErrNotExist
	case http.StatusConflict:
		if action == "file" || action == "read" || action == "restore" {
			return nil, storage.ErrFileChanged
		}
		return nil, fs.ErrExist
	case http.StatusRequestEntityTooLarge:
		return nil, storage.ErrUploadTooLarge
	default:
		return nil, fmt.Errorf("managed %s: HTTP %d", action, res.StatusCode)
	}
}

func (c *Client) get(ctx context.Context, action string, params url.Values, result any) error {
	body, err := c.Request(ctx, http.MethodGet, action, params, nil)
	if err != nil {
		return err
	}
	defer body.Close()
	return json.NewDecoder(body).Decode(result)
}

func (c *Client) CheckAccess(ctx context.Context) error {
	return c.get(ctx, "access", nil, &c.writable)
}

func (c *Client) WritableMount() bool { return c.writable }

func (c *Client) ReadDir(ctx context.Context, dir string) ([]service.DriveEntry, error) {
	var objects []storage.Object
	prefix := dir
	if prefix != "" {
		prefix += "/"
	}
	err := c.get(ctx, "list", url.Values{"prefix": {prefix}}, &objects)
	if err != nil {
		return nil, err
	}
	return service.DriveEntries(prefix, objects), nil
}

func (c *Client) Stat(ctx context.Context, key string) (storage.FileInfo, error) {
	var file storage.FileInfo
	err := c.get(ctx, "stat", url.Values{"path": {key}}, &file)
	return file, err
}

func (c *Client) ReadRange(ctx context.Context, file storage.FileInfo, offset int64, buffer []byte) (int, error) {
	if offset < 0 {
		return 0, fs.ErrInvalid
	}
	if offset >= file.Size {
		return 0, nil
	}
	buffer = buffer[:min(int64(len(buffer)), file.Size-offset)]
	total := 0
	for len(buffer) > 0 {
		chunk := buffer[:min(len(buffer), 16<<20)]
		body, err := c.Request(ctx, http.MethodGet, "read", url.Values{
			"path": {file.Key}, "revision": {file.Revision},
			"offset": {strconv.FormatInt(offset+int64(total), 10)}, "size": {strconv.Itoa(len(chunk))},
		}, nil)
		if err != nil {
			return total, err
		}
		n, err := io.ReadFull(body, chunk)
		body.Close()
		total += n
		if err != nil {
			return total, err
		}
		buffer = buffer[n:]
	}
	return total, nil
}

func (c *Client) CommitWrite(ctx context.Context, key, revision, id string, _ bool, body io.ReadSeeker) error {
	response, err := c.Request(ctx, http.MethodPut, "file", url.Values{"path": {key}, "revision": {revision}, "commit": {id}}, body)
	if err != nil {
		return err
	}
	return response.Close()
}

func (c *Client) CommitDelete(ctx context.Context, key, revision string) error {
	response, err := c.Request(ctx, http.MethodDelete, "file", url.Values{"path": {key}, "revision": {revision}}, nil)
	if err != nil {
		return err
	}
	return response.Close()
}
