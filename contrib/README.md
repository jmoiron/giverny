The systemd service and nginx conf here are set up to let you:

* install giverny by copying the binary to /opt/giverny/
* set up /opt/giverny/media for uploads
* set up /opt/giverny/static for its static files (optional)
* deploy giverny behind an nginx multi-site setup w/ SSL
* run/manage it with systemd

Most of this is probably necessary whether or not you're using docker.

