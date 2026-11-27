# FreeSync

FreeSync turns your own cloud or server storage into a drive on your computer. Its read-only macOS prototype lets you browse remote files in Finder and fetch file data on demand. You can connect S3, Azure Blob, a VPS over SFTP, or Cloudinary.

## Install from source

### macOS with Finder mounting

1. Install [Go](https://go.dev/dl/) 1.27.4 or later. Confirm it is on your PATH with `go version`.
2. Install the Xcode Command Line Tools if you haven't already. Run `xcode-select --install` and finish the installer.
3. Download and install [macFUSE](https://github.com/macfuse/macfuse/wiki/Getting-Started). Follow its instructions for your macOS version, including any required system approval or restart. macFUSE supplies both the headers used to build FreeSync and the runtime used to mount the drive.
4. Open a terminal in the FreeSync checkout and build the executable.

   ```sh
   CGO_ENABLED=1 go build -tags macfuse -o freesync ./cmd/freesync
   ./freesync --help
   ```

### CLI without mounting

For provider setup and listing only, install [Go](https://go.dev/dl/) 1.27.4 or later and run the following from the checkout. This build does not require macFUSE.

```sh
go build -o freesync ./cmd/freesync
./freesync --help
```

On Windows, use `go build -o freesync.exe ./cmd/freesync` and run `.\freesync.exe` in PowerShell in place of `./freesync`.

## Connect your storage

Run `./freesync storage setup` and choose a provider. Use an existing bucket, container, server directory, or Cloudinary account.

| Enter at the prompt | What you need |
| --- | --- |
| `s3` | Bucket name. Set up AWS credentials and a region through the [AWS SDK's standard configuration](https://docs.aws.amazon.com/sdk-for-go/v2/developer-guide/configure-gosdk.html). |
| `azure-blob` | HTTPS storage account URL and container name. Sign in using an identity available to the [Azure default credential chain](https://learn.microsoft.com/en-us/azure/storage/blobs/storage-blob-go-get-started). |
| `sftp` | Server address including its port, SSH username, absolute remote directory, and a local private-key path. FreeSync defaults to `~/.ssh/known_hosts` for host verification. |
| `cloudinary` | Cloud name, API key, and API secret. The terminal hides the secret during entry. FreeSync saves it in the local, unencrypted `storage.json` settings file, which it creates with owner-only permissions on Unix. |

Setup verifies that it can list objects before saving. Run `./freesync storage list` to list remote files or rerun setup to switch providers.

## Mount and eject the drive

With the macFUSE-enabled build, run:

```sh
./freesync storage mount
```

In Finder, press **Shift–Command–G**, enter `~/FreeSync`, and press Return. FreeSync creates this mount location if needed. If it already exists, it must be an empty directory. Keep the mount command running while you use the drive. Eject the volume in Finder or press **Ctrl+C** in the terminal when you're done.

The drive is read-only. A separate local sync folder is unnecessary. Live Finder behavior and provider access still need end-to-end verification, including whether Cloudinary serves partial content for authenticated raw assets.
