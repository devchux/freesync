package storage

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path"
	"strings"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

const uploadPrefix = ".freesync-upload-"

// SFTP stores files under a dedicated, trusted remote directory.
type SFTP struct {
	address string
	root    string
	config  *ssh.ClientConfig
}

// NewSFTP configures a VPS endpoint (host:port) with public key authentication
// and host verification against an OpenSSH known_hosts file.
func NewSFTP(address, user, root, knownHostsFile string, signer ssh.Signer) (*SFTP, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil || host == "" || port == "" {
		return nil, errors.New("SFTP address must be host:port")
	}
	if strings.TrimSpace(user) == "" || signer == nil {
		return nil, errors.New("SFTP user and SSH signer are required")
	}
	if !path.IsAbs(root) || strings.ContainsRune(root, 0) {
		return nil, errors.New("SFTP root must be an absolute remote path")
	}
	verifyHost, err := knownhosts.New(knownHostsFile)
	if err != nil {
		return nil, fmt.Errorf("load SFTP known hosts: %w", err)
	}
	return &SFTP{
		address: address,
		root:    path.Clean(root),
		config: &ssh.ClientConfig{
			User:            user,
			Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
			HostKeyCallback: verifyHost,
		},
	}, nil
}

func (s *SFTP) connect(ctx context.Context) (*sftp.Client, func(), error) {
	dialer := net.Dialer{Timeout: 15 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", s.address)
	if err != nil {
		return nil, nil, fmt.Errorf("dial SFTP server: %w", err)
	}
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	closeConn := func() {
		stop()
		conn.Close()
	}
	fail := func(err error) (*sftp.Client, func(), error) {
		closeConn()
		return nil, nil, fmt.Errorf("open SFTP session: %w", errors.Join(err, ctx.Err()))
	}

	if err := conn.SetDeadline(time.Now().Add(dialer.Timeout)); err != nil {
		return fail(err)
	}
	transport, channels, requests, err := ssh.NewClientConn(conn, s.address, s.config)
	if err != nil {
		return fail(err)
	}
	client, err := sftp.NewClient(ssh.NewClient(transport, channels, requests))
	if err != nil {
		return fail(err)
	}
	if err := conn.SetDeadline(time.Time{}); err != nil {
		return fail(err)
	}
	return client, closeConn, nil
}

func validSFTPKey(key string) bool {
	for part := range strings.SplitSeq(key, "/") {
		if part == "" || part == "." || part == ".." || strings.ContainsRune(part, 0) || strings.HasPrefix(part, uploadPrefix) {
			return false
		}
	}
	return true
}

// Put stages the upload alongside its destination and publishes it with the
// OpenSSH POSIX rename extension after the file has been fully written.
func (s *SFTP) Put(ctx context.Context, key string, body io.ReadSeeker) error {
	if !validSFTPKey(key) {
		return fmt.Errorf("invalid SFTP key %q", key)
	}
	client, closeConn, err := s.connect(ctx)
	if err != nil {
		return err
	}
	defer closeConn()

	if _, ok := client.HasExtension("posix-rename@openssh.com"); !ok {
		return errors.New("SFTP server must support posix-rename@openssh.com for uploads")
	}
	target := path.Join(s.root, key)
	dir := path.Dir(target)
	if err := client.MkdirAll(dir); err != nil {
		return fmt.Errorf("create SFTP directory for %q: %w", key, err)
	}
	temp := path.Join(dir, uploadPrefix+rand.Text())
	file, err := client.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL)
	if err != nil {
		return fmt.Errorf("stage SFTP object %q: %w", key, err)
	}
	_, copyErr := io.Copy(file, body)
	if err := errors.Join(copyErr, file.Close()); err != nil {
		client.Remove(temp)
		return fmt.Errorf("write SFTP object %q: %w", key, errors.Join(err, ctx.Err()))
	}
	if err := client.PosixRename(temp, target); err != nil {
		client.Remove(temp)
		return fmt.Errorf("publish SFTP object %q: %w", key, errors.Join(err, ctx.Err()))
	}
	return nil
}

type sftpBody struct {
	*sftp.File
	closeConn func()
}

func (b *sftpBody) Close() error {
	defer b.closeConn()
	return b.File.Close()
}

// Get streams a remote file. Closing the body also closes the SSH connection.
func (s *SFTP) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	if !validSFTPKey(key) {
		return nil, fmt.Errorf("invalid SFTP key %q", key)
	}
	client, closeConn, err := s.connect(ctx)
	if err != nil {
		return nil, err
	}
	file, err := client.Open(path.Join(s.root, key))
	if err != nil {
		closeConn()
		return nil, fmt.Errorf("get SFTP object %q: %w", key, errors.Join(err, ctx.Err()))
	}
	return &sftpBody{File: file, closeConn: closeConn}, nil
}

// List recursively returns regular files whose relative keys start with prefix.
func (s *SFTP) List(ctx context.Context, prefix string) ([]Object, error) {
	client, closeConn, err := s.connect(ctx)
	if err != nil {
		return nil, err
	}
	defer closeConn()

	objects := make([]Object, 0)
	walker := client.Walk(s.root)
	for walker.Step() {
		if err := errors.Join(walker.Err(), ctx.Err()); err != nil {
			return nil, fmt.Errorf("list SFTP objects: %w", err)
		}
		if walker.Path() == s.root || !walker.Stat().Mode().IsRegular() {
			continue
		}
		key := strings.TrimPrefix(walker.Path(), strings.TrimSuffix(s.root, "/")+"/")
		if validSFTPKey(key) && strings.HasPrefix(key, prefix) {
			objects = append(objects, Object{
				Key:          key,
				Size:         walker.Stat().Size(),
				LastModified: walker.Stat().ModTime(),
			})
		}
	}
	return objects, nil
}

// Delete removes the remote file at key.
func (s *SFTP) Delete(ctx context.Context, key string) error {
	if !validSFTPKey(key) {
		return fmt.Errorf("invalid SFTP key %q", key)
	}
	client, closeConn, err := s.connect(ctx)
	if err != nil {
		return err
	}
	defer closeConn()

	if err := client.Remove(path.Join(s.root, key)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("delete SFTP object %q: %w", key, errors.Join(err, ctx.Err()))
	}
	return nil
}
