# vim: set ft=nginx :

upstream giverny {
    server localhost:7100 fail_timeout=0;
}

server {
    listen 443 ssl;
    server_name giverny.example.com;
    client_max_body_size 16m;

    # if you aren't using letsencrypt then.. why not?
    ssl_certificate         /etc/letsencrypt/live/giverny.example.com/fullchain.pem;
    ssl_certificate_key     /etc/letsencrypt/live/giverny.example.com/privkey.pem;
    ssl_trusted_certificate /etc/letsencrypt/live/giverny.example.com/fullchain.pem;

    ssl_session_cache shared:SSL:50m;
    ssl_session_timeout 5m;
    ssl_stapling on;
    ssl_stapling_verify on;

    ssl_protocols TLSv1 TLSv1.1 TLSv1.2;
    ssl_ciphers "ECDHE-RSA-AES256-GCM-SHA384:ECDHE-RSA-AES128-GCM-SHA256:DHE-RSA-AES256-GCM-SHA384:DHE-RSA-AES128-GCM-SHA256:ECDHE-RSA-AES256-SHA384:ECDHE-RSA-AES128-SHA256:ECDHE-RSA-AES256-SHA:ECDHE-RSA-AES128-SHA:DHE-RSA-AES256-SHA256:DHE-RSA-AES128-SHA256:DHE-RSA-AES256-SHA:DHE-RSA-AES128-SHA:ECDHE-RSA-DES-CBC3-SHA:EDH-RSA-DES-CBC3-SHA:AES256-GCM-SHA384:AES128-GCM-SHA256:AES256-SHA256:AES128-SHA256:AES256-SHA:AES128-SHA:DES-CBC3-SHA:HIGH:!aNULL:!eNULL:!EXPORT:!DES:!MD5:!PSK:!RC4";

    ssl_dhparam /etc/nginx/dhparams.pem;
    ssl_prefer_server_ciphers on;

    gzip on;
    gzip_min_length 1000;

    # optional; nginx is good at serving statics, but this will mean that
    # updating giverny will require you to update the static dir
    location ^~/static/ {
        alias /opt/giverny/static/;
        expires 10m;
    }

    location ^~/media/ {
        alias /opt/giverny/media/;
        expires 1d;
    }

    location / {
        proxy_pass http://giverny;
    }

    access_log  /var/log/nginx/giverny.example.com/access.log;
    error_log   /var/log/nginx/giverny.example.com/error.log;
}
