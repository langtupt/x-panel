"""Local UI fixture server. No connection to VPS; all mutations are simulated."""
from http.server import ThreadingHTTPServer, SimpleHTTPRequestHandler
from pathlib import Path
import json,time,math
ROOT=Path(__file__).resolve().parents[2]/"apps/web/out"
started=time.time();task=None;reboot_at=0
names=[("php","PHP","runtime",["8.5","8.4","8.3"],["8.3"]),("composer","Composer","runtime",["2.10.3"],["2.10.3"]),("nodejs","Node.js","runtime",["26","24","22"],["24"]),("go","Go","runtime",["1.27.2"],[]),("mariadb","MariaDB","database",[],["10.11.14"]),("postgresql","PostgreSQL","database",["18","17","16"],["18"]),("phpmyadmin","phpMyAdmin","tool",["5.2.3"],["5.2.3"]),("adminer","Adminer","tool",["6.1.2"],["6.1.2"]),("redis","Redis","cache",["7.0.15"],[]),("memcached","Memcached","cache",["1.6.24"],[])]
apps=[dict(id=i,name=n,category=c,available=v,installed=s,builtin=i=="mariadb",description={"php":"PHP-FPM chạy song song nhiều phiên bản. Chọn dòng PHP phù hợp cho website.","redis":"Cache dùng chung trên localhost, cài từ kho hệ điều hành."}.get(i,"Runtime, cơ sở dữ liệu và công cụ quản trị cho website."),source="Nguồn phát hành chính thức",checked_at="2026-10-19T09:00:00Z",version_labels={"8.5":"8.5 → 8.5.11"} if i=="php" else {}) for i,n,c,v,s in names]
class Handler(SimpleHTTPRequestHandler):
 def __init__(self,*args,**kw):super().__init__(*args,directory=str(ROOT),**kw)
 def log_message(self,*args):pass
 def send(self,data,status=200):
  self.send_response(status);self.send_header("Content-Type","application/json");self.end_headers();self.wfile.write(json.dumps(data).encode())
 def do_GET(self):
  global task
  path=self.path.split("?")[0]
  if not path.startswith("/api/"):return super().do_GET()
  if path=="/api/me":return self.send(dict(id=1,username="admin",role="admin",email="admin@example.test",impersonator=None,status="active",totp_enabled=False))
  if path=="/api/apps":return self.send(dict(apps=apps))
  if path=="/api/system/info":return self.send(dict(hostname="xpanel-server",os_name="Ubuntu 24.04 LTS",kernel="6.8.0-147-generic",cpu_count=4,cpu_percent=12+math.sin(time.time())*3,cpu_model="Intel Xeon Processor",architecture="amd64",boot_id="new" if reboot_at and time.time()-reboot_at>8 else "preview",uptime_sec=92342,load=[.12,.18,.09],mem_total_mb=4096,mem_used_mb=1536,swap_total_mb=2048,swap_used_mb=80,disk_total_mb=80000,disk_used_mb=19000,disk_available_mb=57000,network_rx_bytes=2134119823,network_tx_bytes=5134119823,quota_enabled=True,quota_mount="/"))
  if path.startswith("/api/tasks"):
   if task:
    elapsed=time.time()-task["start"]
    task["status"]="done" if elapsed>9 else "running"
    task["log"]="Đang kiểm tra kho gói…\nĐã xác minh checksum.\nĐang cài đặt runtime…"+("\nHoàn tất thành công." if elapsed>9 else "")
   return self.send((task or {}) if path!="/api/tasks" else ([task] if task else []))
  if path=="/api/packages":return self.send(dict(packages=[],php_versions=["8.5","8.4","8.3"]))
  if path=="/api/php":return self.send(dict(installed=["8.3"],supported=["8.5","8.4","8.3"]))
  if path=="/api/users":return self.send(dict(users=[],quota_enabled=True))
  if path=="/api/phpmyadmin":return self.send(dict(installed=True,php_version="8.3"))
  if path=="/api/files":return self.send(dict(entries=[],cwd="/"))
  if path=="/api/me/overview":return self.send(dict(package=None,quota_enabled=True,disk_used_mb=0))
  return self.send([])
 def do_POST(self):
  global task,reboot_at
  data=json.loads(self.rfile.read(int(self.headers.get("Content-Length",0))) or b"{}")
  if self.path=="/api/system/reboot":
   if data.get("confirmation")!="REBOOT":return self.send(dict(error="confirmation required"),400)
   reboot_at=time.time();return self.send(dict(scheduled=True,delay_seconds=10),202)
  if "/install" in self.path:
   task=dict(id=101,user_id=1,type="app.install",target=self.path.split("/")[3]+" "+data["version"],status="pending",log="Yêu cầu đã được tiếp nhận.",created_at="2026-10-19T09:00:00Z",started_at=None,finished_at=None,start=time.time())
   return self.send(task,202)
  return self.send({})
print("UI fixture at http://127.0.0.1:3010",flush=True)
ThreadingHTTPServer(("127.0.0.1",3010),Handler).serve_forever()
