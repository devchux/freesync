# FreeSync

FreeSync turns your own cloud or server storage into a drive on your computer. Its mount presents remote files in Finder, Windows File Explorer, or a Linux file manager and fetches data on demand. You can connect S3, Azure Blob, a VPS over SFTP, or Cloudinary.

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

The default mount is read-only. A separate local sync folder is unnecessary. Live mount behavior and provider access still need end-to-end verification, including whether Cloudinary serves partial content for authenticated raw assets.

## Native writable mounts

Connect an S3 bucket or Azure Blob container with `freesync storage setup`, then use the mount-enabled build for your platform:

```sh
freesync storage mount --write
```

On macOS or Linux, `make mount-write` builds and runs the same command. For Azure or S3, the connection needs permission to list, read, write, and delete files. SFTP and Cloudinary currently use read-only mounts.

FreeSync stores queue metadata in the database selected by `freesync setup`. SQLite, PostgreSQL, and MySQL all work. A drive-only setup without saved database settings uses the default SQLite database. Staged file contents stay on the machine that edited them, even with a shared database.

FreeSync resumes queued operations when you remount the same storage connection using the same database. Let pending operations finish before switching databases. If the database becomes unavailable while recording a change, FreeSync keeps the local data. Restore the database connection and remount to reconcile the queue. A successful save means FreeSync accepted the local change. It can take longer for the upload to reach storage. If the remote file changes before a queued save commits, FreeSync keeps the local copy and prints its location. Pending files and conflicts live under `freesync/writes` in your OS configuration directory. Keep this directory while uploads are pending. FreeSync does not enable cloud versioning.

File renames publish the destination before conditionally deleting the source. Both objects may be visible to other clients until the delete succeeds. Directory creation, rename, and deletion are outside this first writable implementation. A file can be busy while an upload is in progress. Opening an existing file for in-place editing or renaming it can require downloading the whole file. Saves and renames write file contents using the destination’s upload defaults. Custom object metadata and storage tiers may need to be reapplied. S3 saves currently use single-object uploads. [S3 upload limits](https://docs.aws.amazon.com/AmazonS3/latest/userguide/upload-objects.html)

## Mount cache, refresh, and upload status

Set cache lifetimes when starting the mount:

```sh
freesync storage mount --data-ttl 30s --metadata-ttl 10s
```

Both flags default to `0`, which disables reuse in the FreeSync cache. The data cache holds recently requested byte ranges up to 256 KiB each. The metadata cache holds directory listings and file metadata. These settings also work with `--write`.

In the terminal running the mount, type `refresh` and press Enter to clear both caches. Refresh or reopen the directory in your file manager to fetch the current listing. Reopen an already open file to read a new remote revision.

On writable mounts, the terminal reports uploads as pending, uploading, committed, conflicted, or pending retry. A committed message means the provider operation completed and FreeSync recorded completion. If an upload conflicts, the terminal also prints where the staged copy was retained.
