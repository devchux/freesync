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

## Self-host a managed drive

A FreeSync server provides per-drive sharing and revision checks. It stores metadata and permissions in SQLite, PostgreSQL, or MySQL and uses the existing storage service for file contents. Run one server per managed database and configure a dedicated bucket, container, or storage root with `freesync storage setup`. The server stores internal objects under `.freesync-managed/`.

Run `freesync managed serve`. SQLite is the default. To use PostgreSQL or MySQL, pass `--db-type postgres` or `--db-type mysql` and `--db CONNECTION_STRING` to `serve` and `restore`. The server listens on loopback by default. To connect from other computers, put it behind an HTTPS reverse proxy that preserves the public Host header and pass `--server https://drive.example.com` to client commands.

Users sign up with a full name, email address, and password. The account that creates a drive owns it and can invite registered users, set their read or write access, and revoke access. An invitee signs in to accept the invitation.

```sh
freesync managed signup --name "Alice Owner" --email alice@example.com
freesync managed create --drive team --email alice@example.com
freesync managed invite --drive team --email alice@example.com --member bob@example.com --write
freesync managed accept --drive team --email bob@example.com
freesync managed mount --drive team --email bob@example.com --write
```

Bob must sign up before Alice invites him. FreeSync prompts for the password in the terminal. For read-only access, omit `--write` from both `invite` and `mount`. To revoke Bob's access, Alice runs `freesync managed revoke --drive team --email alice@example.com --member bob@example.com`.

Email addresses identify accounts on this server. The server does not send email verification messages or invitations.

The mount reports pending, committed, and conflicted saves in its terminal. If two users edit the same revision, the first commit becomes the current file and FreeSync prints the location of the other user's staged copy. Copy that staged file before resolving the conflict. The server checks revisions with file history off.

### Browser API

Serve the web client and API from the same HTTPS origin. Send JSON to `POST /auth/signup` with `fullName`, `email`, and `password`, then to `POST /auth/login` with `email` and `password`. Login sets an HTTP-only session cookie. Use `GET /auth/me` to load the signed-in account and `POST /auth/logout` to end the session.

`GET /drives` lists owned drives, accepted access, and pending invitations for the account. Use `POST /drives/{drive}/create` to create a drive, `POST /drives/{drive}/accept` to accept an invitation, and `GET /drives/{drive}/members` to list its members as the owner. The owner can call `POST /drives/{drive}/invite?email=...&write=true` and `POST /drives/{drive}/revoke?email=...`.

### Export and recovery

A member can run `freesync managed export --drive team --email bob@example.com --file team.tar` to download a tar archive of ordinary files. Configure the same random `FREESYNC_MANAGED_ADMIN_TOKEN` of at least 32 characters on the server and administrative client to enable backup and legacy ownership recovery. An administrator can run `freesync managed backup --file server.tar` to save all drives, current file contents, and managed-drive metadata, including accounts, password hashes, ownership, permissions, and invitations. Users sign in again after restore. The backup format works across all three database engines. The server protects the snapshot against concurrent writes while streaming it.

On a fresh server, configure an empty storage backend and run `freesync managed restore --db /path/to/managed.sqlite --file server.tar` with a database that has no managed drives or accounts. For PostgreSQL and MySQL, create the database first and pass its connection string instead of a file path. Start the server with `freesync managed serve --db /path/to/managed.sqlite` and its configured admin token. Member email addresses and passwords continue working with the restored database. Preserve client spool files separately if they contain pending or conflicted edits.

For a drive created before account ownership, first register the intended owner and run `freesync managed claim --drive team --email alice@example.com` as the server administrator. The new owner can invite members through their accounts.
