package web

import "text/template"

// Every value rendered here has been validated (domain, doc root, username,
// PHP version), so the templates do not need escaping.

type vhostData struct {
	Port          int
	Username      string
	Domain        string
	Names         string // server_name list
	Root          string
	PHPSocket     string // empty = static site
	SSL           bool
	CertFile      string
	KeyFile       string
	Suspended     bool
	OverBandwidth bool
	ACMERoot      string
	LogDir        string
}

var vhostTmpl = template.Must(template.New("vhost").Parse(`# Managed by X-Panel ({{.Username}}): changes will be overwritten.
{{define "body"}}
    root {{.Root}};
    index index.php index.html index.htm;
    charset utf-8;
    client_max_body_size 128m;
    # nginx is in every customer's group: never follow a customer's symlink into
    # files owned by someone else (e.g. ~/www/x -> /home/other/www/.env).
    disable_symlinks if_not_owner from=$document_root;
    access_log {{.LogDir}}/{{.Domain}}.access.log;
    error_log  {{.LogDir}}/{{.Domain}}.error.log;
{{if .Suspended}}
    location / {
        default_type text/html;
        return 503 "<!doctype html><meta charset=utf-8><title>Tạm ngưng</title><h1>Website tạm ngưng hoạt động</h1>";
    }
{{else if .OverBandwidth}}
    location / {
        default_type text/html;
        add_header Retry-After 86400 always;
        return 503 "<!doctype html><meta charset=utf-8><title>Hết băng thông</title><h1>Website đã dùng hết băng thông của tháng này</h1><p>Bandwidth limit exceeded.</p>";
    }
{{else}}
    location ~ /\.(?!well-known) { deny all; }
{{- if .Port}}
    location / {
        proxy_pass http://127.0.0.1:{{.Port}};
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection $xpanel_conn_upgrade;
        proxy_read_timeout 300s;
    }
{{- else if .PHPSocket}}
    location / {
        try_files $uri $uri/ /index.php?$query_string;
    }
    location ~ \.php$ {
        try_files $uri =404;
        fastcgi_split_path_info ^(.+\.php)(/.+)$;
        fastcgi_pass unix:{{.PHPSocket}};
        fastcgi_index index.php;
        include fastcgi_params;
        fastcgi_param SCRIPT_FILENAME $document_root$fastcgi_script_name;
        fastcgi_param PATH_INFO $fastcgi_path_info;
        fastcgi_read_timeout 300s;
    }
{{- else}}
    location / {
        try_files $uri $uri/ =404;
    }
{{- end}}
{{end}}
{{- end}}
server {
    listen 80;
    listen [::]:80;
    server_name {{.Names}};

    location ^~ /.well-known/acme-challenge/ {
        root {{.ACMERoot}};
        default_type text/plain;
    }
{{- if .SSL}}
    location / {
        return 301 https://$host$request_uri;
    }
}

server {
    listen 443 ssl http2;
    listen [::]:443 ssl http2;
    server_name {{.Names}};

    ssl_certificate     {{.CertFile}};
    ssl_certificate_key {{.KeyFile}};
    ssl_protocols       TLSv1.2 TLSv1.3;
    ssl_session_cache   shared:xpanel_sites:20m;
{{template "body" .}}}
{{- else}}
{{template "body" .}}}
{{- end}}
`))

type poolData struct {
	RunDir      string
	Unit        string
	Username    string
	Home        string
	Socket      string
	NginxUser   string
	MaxChildren int
}

// The master runs as root inside the account's slice; its own log goes to
// syslog so it never writes into a customer-controlled directory.
var poolTmpl = template.Must(template.New("pool").Parse(`; Managed by X-Panel: changes will be overwritten.
[global]
pid = {{.RunDir}}/php-fpm.pid
error_log = syslog
syslog.ident = {{.Unit}}
daemonize = no

[{{.Username}}]
user = {{.Username}}
group = {{.Username}}
listen = {{.Socket}}
listen.owner = {{.NginxUser}}
listen.group = {{.NginxUser}}
listen.mode = 0660

pm = ondemand
pm.max_children = {{.MaxChildren}}
pm.process_idle_timeout = 30s
pm.max_requests = 500
chdir = /

php_admin_value[open_basedir] = {{.Home}}/:/tmp/
php_admin_value[upload_tmp_dir] = {{.Home}}/tmp
php_admin_value[sys_temp_dir] = {{.Home}}/tmp
php_admin_value[session.save_path] = {{.Home}}/tmp
php_admin_value[error_log] = {{.Home}}/logs/php-error.log
php_admin_flag[log_errors] = on
`))

const welcomePage = `<!doctype html>
<html lang="vi"><head><meta charset="utf-8"><title>Website đang được xây dựng</title>
<style>body{font-family:system-ui,sans-serif;display:grid;place-items:center;min-height:100vh;margin:0;background:#f8fafc;color:#334155}</style>
</head><body><div><h1>Website đang được xây dựng</h1><p>Trang này được tạo bởi X-Panel. Hãy tải mã nguồn của bạn lên để thay thế.</p></div></body></html>
`

// unitTmpl is the systemd template for one PHP version; the instance is the
// username, so each account gets its own master in xpanel-<user>.slice.
var unitTmpl = template.Must(template.New("unit").Parse(`# Managed by X-Panel: changes will be overwritten.
[Unit]
Description=PHP-FPM {{.Version}} for %i (X-Panel)
After=network.target

[Service]
Type=notify
ExecStart={{.Binary}} --nodaemonize --fpm-config /etc/xpanel/php/{{.Version}}/%i.conf
ExecReload=/bin/kill -USR2 $MAINPID
Slice=xpanel-%i.slice
RuntimeDirectory=xpanel-php{{.Version}}-%i
RuntimeDirectoryMode=0755
# A worker killed for exceeding the account's MemoryMax must not take the
# master (and every other request of the account) down with it.
OOMPolicy=continue
# Account isolation: own /tmp, and workers cannot see other accounts' processes.
PrivateTmp=true
ProtectProc=invisible
Restart=on-failure
RestartSec=2

[Install]
WantedBy=multi-user.target
`))

type unitData struct {
	Version string
	Binary  string
}
