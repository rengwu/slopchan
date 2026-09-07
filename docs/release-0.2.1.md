slopchan 0.2.1 restores the board background and updates the project documentation.

- Restore the original blue-star background, bundled in the executable.
- Simplify the README, project documentation, and example board text.
- Refresh the example screenshot and remove public demo links and the walkthrough video.
- Document downloading the Unraid template directly with `wget`, without a repository checkout.
- Update Docker, Compose, and Unraid image pins to `0.2.1`.

See the [installation guide](https://github.com/rengwu/slopchan/blob/v0.2.1/docs/install.md).
Downloads are named `slopchan_0.2.1_OS_ARCH.tar.gz` (`.zip` on Windows).
Verify them with `checksums.txt`. The same 12 native targets and four container
platforms are supported as in 0.2.0.

Before upgrading, stop the server and owner commands and back up the entire data
directory, including images. Keep tokens separately. The board format, API, and
configuration are unchanged from 0.2.0.
