$ErrorActionPreference = "SilentlyContinue"
# 只清理验收脚本启动的 headless 实例（带 remote-debugging-port / gm09 临时 profile），不触碰用户日常浏览器。
$procs = Get-CimInstance Win32_Process -Filter "Name='msedge.exe'" | Where-Object { $_.CommandLine -match "remote-debugging-port=5[23]" -or $_.CommandLine -match "gm09-(live|stub|probe)" }
foreach ($p in $procs) {
  Write-Output ("killing msedge pid=" + $p.ProcessId)
  Stop-Process -Id $p.ProcessId -Force
}
$nodes = Get-CimInstance Win32_Process -Filter "Name='node.exe'" | Where-Object { $_.CommandLine -match "run-(live-controller-)?acceptance|stub-server" }
foreach ($p in $nodes) {
  Write-Output ("killing node pid=" + $p.ProcessId)
  Stop-Process -Id $p.ProcessId -Force
}
$ctrl = Get-CimInstance Win32_Process -Filter "Name='controller.exe'" | Where-Object { $_.CommandLine -match "controller-live" }
foreach ($p in $ctrl) {
  Write-Output ("killing controller pid=" + $p.ProcessId)
  Stop-Process -Id $p.ProcessId -Force
}
Write-Output "cleanup done"
