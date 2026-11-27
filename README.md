# FreeSync

FreeSync turns your own cloud or server storage into a drive on your computer. Its read-only mount presents remote files in Finder, Windows File Explorer, or a Linux file manager and fetches data on demand. You can connect S3, Azure Blob, a VPS over SFTP, or Cloudinary.

## Install from source

### With Make

Run `make help` from the FreeSync checkout to see the available commands.

For Finder mounting, install [Homebrew](https://brew.sh/) and the Xcode Command Line Tools (`xcode-select --install`), then run:

```sh
make deps-macos
make install-macos
make setup
make mount
```

`make deps-macos` installs Go and macFUSE when missing and downloads the Go modules. Complete any system approval or restart macFUSE requests before mounting. [macFUSE setup guide](https://github.com/macfuse/macfuse/wiki/Getting-Started)

For the CLI alone, install Go, then run `make deps`, `make install`, and `make setup`. Use `make list` to list remote objects. The install targets put `freesync` in `~/.local/bin` by default. Add that directory to your PATH to run `freesync` from anywhere, or override the destination with `make install PREFIX=/your/prefix`.

### Linux with FUSE 3

1. Install [Go](https://go.dev/dl/) 1.27.4 or later, Make, a C compiler, and FUSE 3 development headers and runtime. On Debian or Ubuntu, install the system packages with:

   ```sh
   sudo apt install make gcc pkg-config fuse3 libfuse3-dev
   ```

2. From the FreeSync checkout, run:

   ```sh
   make deps-linux
   make install-linux
   make setup
   make mount
   ```

3. Open `~/FreeSync` in your file manager. To unmount, press **Ctrl+C** in the terminal or run `fusermount3 -u "$HOME/FreeSync"` in a second terminal. The host must allow user mounts through `/dev/fuse`.

### Windows with WinFsp

1. Install [WinFsp](https://winfsp.dev/rel/) on the Windows machine. If you're building there, also install [Go](https://go.dev/dl/) 1.27.4 or later.
2. From the FreeSync checkout in PowerShell, build and run the mount-enabled CLI:

   ```powershell
   $env:CGO_ENABLED = "0"
   go build -tags winfsp -o freesync.exe ./cmd/freesync
   .\freesync.exe storage setup
   .\freesync.exe storage mount
   ```

   To build the Windows executable from macOS or Linux, run `make build-windows` and copy `bin/freesync-windows-amd64.exe` to the Windows machine.

3. Open the drive letter printed by FreeSync in File Explorer. The mount uses the first available letter from `F:` through `Z:`. Press **Ctrl+C** in the terminal to unmount. Remote paths are case-sensitive. The mount hides Windows reserved names and names with unsupported characters.

### Manual macOS installation with Finder mounting

1. Install [Go](https://go.dev/dl/) 1.27.4 or later. Confirm it is on your PATH with `go version`.
2. Install the Xcode Command Line Tools if you haven't already. Run `xcode-select --install` and finish the installer.
3. Download and install [macFUSE](https://github.com/macfuse/macfuse/wiki/Getting-Started). Follow its instructions for your macOS version, including any required system approval or restart. macFUSE supplies both the headers used to build FreeSync and the runtime used to mount the drive.
4. Open a terminal in the FreeSync checkout and build the executable.

   ```sh
   CGO_ENABLED=1 go build -tags macfuse -o freesync ./cmd/freesync
   ./freesync --help
   ```

### Manual CLI installation without mounting

For provider setup and listing only, install [Go](https://go.dev/dl/) 1.27.4 or later and run the following from the checkout. This build does not require macFUSE.

```sh
go build -o freesync ./cmd/freesync
./freesync --help
```

On Windows, use `go build -o freesync.exe ./cmd/freesync` and run `.\freesync.exe` in PowerShell in place of `./freesync`.

## Connect your storage

Run `make setup` or `./freesync storage setup` and choose a provider. Use an existing bucket, container, server directory, or Cloudinary account.

| Enter at the prompt | What you need |
| --- | --- |
| `s3` | Bucket name. Set up AWS credentials and a region through the [AWS SDK's standard configuration](https://docs.aws.amazon.com/sdk-for-go/v2/developer-guide/configure-gosdk.html). |
| `azure-blob` | HTTPS storage account URL and container name. Sign in using an identity available to the [Azure default credential chain](https://learn.microsoft.com/en-us/azure/storage/blobs/storage-blob-go-get-started). |
| `sftp` | Server address including its port, SSH username, absolute remote directory, and a local private-key path. FreeSync defaults to `~/.ssh/known_hosts` for host verification. |
| `cloudinary` | Cloud name, API key, and API secret. The terminal hides the secret during entry. FreeSync saves it in the local, unencrypted `storage.json` settings file, which it creates with owner-only permissions on Unix. |

Setup verifies that it can list objects before saving. Run `./freesync storage list` to list remote files or rerun setup to switch providers.

## Mount and eject the drive on macOS

Run `make mount` from the checkout, or invoke your macFUSE-enabled binary directly:

```sh
./freesync storage mount
```

In Finder, press **Shift–Command–G**, enter `~/FreeSync`, and press Return. FreeSync creates this mount location if needed. If it already exists, it must be an empty directory. Keep the mount command running while you use the drive. Eject the volume in Finder or press **Ctrl+C** in the terminal when you're done.

The drive is read-only. A separate local sync folder is unnecessary. Live mount behavior and provider access still need end-to-end verification, including whether Cloudinary serves partial content for authenticated raw assets.
