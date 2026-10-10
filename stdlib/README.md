# Source libraries

These are small, experimental Linglang packages. Import them using a relative
path from the importing file. `http` implements bounded HTTP/1.1 requests with
no request bodies and a close-after-response policy; it is not a general HTTP
framework. `sqlite` provides constructors for bound SQL parameter values.

For reusable dependencies, copy the package sources (preserving relative layout)
under a project's `vendor/` directory and commit them with their license and
upstream commit ID. Builds never contact the network. The initial package model
has no registry, version solver, automatic downloader, or hidden installation
scripts. This is the reproducible local foundation for those future tools.

Use the seed CLI `linglang deps <project>` to record imported source hashes in
`linglang.lock`, then run `linglang deps --check <project>` in CI. Builds do not
automatically enforce or modify the lock.
