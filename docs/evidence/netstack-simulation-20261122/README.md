# 网络栈模拟证据（2026-11-22）

本目录是只含文本的实验源与结果，不是开发安装包。产品保留Tailscale1.98.10/Go1.26.11；1.104.2/Go1.27.4只在隔离候选中测试，未部署，当前不采用作卡顿修复。

见 [完整报告](REPORT.md)、[原始配对结果](results.json)及[控制结果](controls-results.json)。两端是gVisor内存协议栈，110ms RTT、5Mbps、MTU1280、单向数据包0/1%丢包、seed901/902、每次2MiB完整SHA校验。没有真实网卡、WireGuard/DERP、HTTP/2、浏览器或Tailscale特定buffer配置；此结果不能反推用户线路丢包，也不能断言实际升级必然退化。只说明缺乏把该升级作为提速方案的证据。

源文件保存为.txt避免被产品Go构建扫描。需要复现时在新的临时目录复制并去掉以下文件的.txt后缀：main.go、run.py、controls.py、go.mod、go.sum、go.old.mod、go.old.sum、go.new.mod、go.new.sum。然后执行：

```sh
GOTOOLCHAIN=go1.27.4 GOMAXPROCS=2 go build -p=1 -modfile=go.old.mod -o old main.go
GOTOOLCHAIN=go1.27.4 GOMAXPROCS=2 go build -p=1 -modfile=go.new.mod -o new main.go
python3 run.py
python3 controls.py
```

会在该临时目录生成两个测试二进制并重写results.json/controls-results.json；请勿在本证据目录直接运行。每样本进程截止40秒，内部35秒；11个原样本均exit0且数据SHA正确。SHA256SUMS覆盖归档证据文本（两份buildinfo仅去掉行尾空白，其余原文保留），不包含本说明自身。REPORT中的复现路径是原实验目录语境，归档后请以上方临时目录步骤为准。原临时目录及实验二进制已清理。
