# Dot-source this file to use project-local runtimes in the current session.
$fleetRoot = Split-Path -Parent $PSScriptRoot
$fleetGo = Join-Path $fleetRoot '.tools/go/bin'
$fleetNode = Join-Path $fleetRoot '.tools/node-v24.22.1-win-x64'
$fleetBins = Join-Path $fleetRoot '.tools/bin'
$env:PATH = "$fleetBins;$fleetGo;$fleetNode;$env:PATH"
$env:GOBIN = $fleetBins
$env:GOPATH = Join-Path $fleetRoot '.cache/gopath'
$env:GOCACHE = Join-Path $fleetRoot '.cache/go-build'
$env:GOTOOLCHAIN = 'local'
