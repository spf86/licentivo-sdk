[CmdletBinding()]
param(
    [ValidateSet('go','java','c','cpp','csharp','python','javascript','rust','ruby')][string]$Language,
    [switch]$CheckOnly, [string]$LicenseKey,
    [string]$Feature = 'export',
    [string]$Output = 'licensed-report.txt', [switch]$Metered, [string]$OperationID,
    [string]$PythonExe = 'python', [string]$CurlRoot
)
$ErrorActionPreference = 'Stop'
function Invoke-Checked([string]$Tool, [string[]]$Arguments) {
    & $Tool @Arguments
    if ($LASTEXITCODE -ne 0) { throw "$Tool failed (exit $LASTEXITCODE). See QUICKSTART.md." }
}
function Require-Tool([string]$Tool, [string[]]$Arguments) {
    if (-not (Get-Command $Tool -ErrorAction SilentlyContinue)) { throw "Missing $Tool. Install the development tools listed in QUICKSTART.md, then retry." }
    Invoke-Checked $Tool $Arguments
}
Push-Location $PSScriptRoot
$saved = @{}
foreach ($name in @('LICENTIVO_FEATURE','LICENTIVO_OUTPUT','LICENTIVO_LICENSE_KEY','LICENTIVO_METERED','LICENTIVO_OPERATION_ID','LICENTIVO_CURL_ROOT','PATH')) { $saved[$name] = [Environment]::GetEnvironmentVariable($name,'Process') }
try {
    if (-not $Language -and (Test-Path -LiteralPath 'SDK-LANGUAGE')) { $Language = (Get-Content -LiteralPath 'SDK-LANGUAGE' -Raw).Trim() }
    if (-not $Language) { throw 'Select -Language go, java, c, cpp, csharp, python, javascript, rust or ruby.' }
    if ($CurlRoot) { $CurlRoot = (Resolve-Path -LiteralPath $CurlRoot).Path; $env:LICENTIVO_CURL_ROOT=$CurlRoot; $env:PATH=(Join-Path $CurlRoot 'bin')+';'+$env:PATH }
    Write-Output "Checking $Language development environment; no activation is sent by -CheckOnly."
    switch ($Language) {
        go { Require-Tool 'go' @('version') }
        java { Require-Tool 'javac' @('-version'); Require-Tool 'java' @('-version') }
        csharp { Require-Tool 'dotnet' @('--version') }
        python { Require-Tool $PythonExe @('-c','import sys; assert sys.version_info >= (3,10), "Python 3.10+ required"; print(sys.version.split()[0])') }
        javascript { Require-Tool 'node' @('-e','if(Number(process.versions.node.split(".")[0])<20)throw Error("Node.js 20+ required");console.log(process.versions.node)') }
        rust { Require-Tool 'cargo' @('--version'); Require-Tool 'gcc' @('--version'); if (-not $env:LICENTIVO_CURL_ROOT) { throw 'Rust on Windows requires the GNU/MinGW target and -CurlRoot <matching libcurl-folder>.' } }
        ruby { Require-Tool 'ruby' @('-e','require "fiddle"; abort "Ruby 3.1+ required" if (RUBY_VERSION.split(".").map(&:to_i)[0,2] <=> [3,1]) == -1; puts RUBY_VERSION'); Require-Tool 'gcc' @('--version'); if (-not $env:LICENTIVO_CURL_ROOT) { throw 'Ruby source builds on Windows require -CurlRoot <matching libcurl-folder>.' } }
        default { if (Get-Command cmake -ErrorAction SilentlyContinue) { Require-Tool 'cmake' @('--version') } elseif ($CurlRoot) { Require-Tool 'gcc' @('--version'); Require-Tool 'g++' @('--version') } else { throw 'C/C++ require CMake and libcurl development files. With MinGW, supply -CurlRoot <curl-folder>.' } }
    }
    if ($CheckOnly) { Write-Output 'Development tools found. No activation was sent. Fill product ID and public keys in the source before running.'; return }
    $env:LICENTIVO_FEATURE = $Feature; $env:LICENTIVO_OUTPUT = $Output
    if ($LicenseKey) { $env:LICENTIVO_LICENSE_KEY=$LicenseKey }
    if ($Metered) { if (-not $OperationID) { throw 'Supply -OperationID with a stable job ID when using -Metered.' }; $env:LICENTIVO_METERED = '1'; $env:LICENTIVO_OPERATION_ID = $OperationID }
    else { $env:LICENTIVO_METERED = '0' }
    switch ($Language) {
        go { $entry = if (Test-Path -LiteralPath 'sdk/go/quickstart') { './sdk/go/quickstart' } else { './examples/go/quickstart' }; Invoke-Checked 'go' @('run',$entry) }
        java { New-Item -ItemType Directory -Force -Path '.local/quickstart/java' | Out-Null; $sources = (Get-ChildItem -LiteralPath 'sdk/java/src/licentivo' -Filter '*.java').FullName; Invoke-Checked 'javac' (@('-encoding','UTF-8','-d','.local/quickstart/java') + $sources); Invoke-Checked 'java' @('-cp','.local/quickstart/java','licentivo.Quickstart') }
        csharp { Invoke-Checked 'dotnet' @('run','--project','sdk/csharp/quickstart') }
        python {
            if (-not (Test-Path -LiteralPath '.venv/Scripts/python.exe')) { Invoke-Checked $PythonExe @('-m','venv','.venv') }
            Invoke-Checked './.venv/Scripts/python.exe' @('-m','pip','install','--disable-pip-version-check','-e','./sdk/python')
            Invoke-Checked './.venv/Scripts/python.exe' @('sdk/python/quickstart.py')
        }
        javascript { Invoke-Checked 'node' @('sdk/javascript/quickstart.mjs') }
        rust { Invoke-Checked 'cargo' @('run','--manifest-path','sdk/rust/Cargo.toml','--example','quickstart') }
        ruby { Invoke-Checked 'ruby' @('sdk/ruby/build_native.rb'); Invoke-Checked 'ruby' @('sdk/ruby/examples/quickstart.rb') }
        default {
            $target = "licentivo_${Language}_quickstart"
            if ($CurlRoot) { $CurlRoot = (Resolve-Path -LiteralPath $CurlRoot).Path; $env:PATH = (Join-Path $CurlRoot 'bin') + ';' + $env:PATH }
            if (Get-Command cmake -ErrorAction SilentlyContinue) {
                $arguments = @('-S','sdk','-B','.local/quickstart/native','-DLICENTIVO_BUILD_TESTS=OFF')
                if ($CurlRoot) { $arguments += @('-G','MinGW Makefiles',"-DCURL_INCLUDE_DIR=$CurlRoot/include","-DCURL_LIBRARY=$CurlRoot/lib/libcurl.dll.a") }
                Invoke-Checked 'cmake' $arguments
                Invoke-Checked 'cmake' @('--build','.local/quickstart/native','--config','Release','--target',$target)
                $executable = Get-ChildItem -LiteralPath '.local/quickstart/native' -Filter "$target.exe" -Recurse | Select-Object -First 1
                if (-not $executable) { throw 'Native build did not produce the quickstart executable.' }
                Invoke-Checked $executable.FullName @()
            } else {
                New-Item -ItemType Directory -Force -Path '.local/quickstart/native' | Out-Null
                $objects = @()
                foreach ($source in @('licentivo','vendor/cJSON','vendor/monocypher','vendor/monocypher-ed25519')) {
                    $object = '.local/quickstart/native/' + ($source.Split('/')[-1]) + '.o'
                    Invoke-Checked 'gcc' @('-std=c11','-O2','-Isdk/c',"-I$CurlRoot/include",'-c',"sdk/c/$source.c",'-o',$object)
                    $objects += $object
                }
                $tool = if ($Language -eq 'c') { 'gcc' } else { 'g++' }; $extension = if ($Language -eq 'c') { 'c' } else { 'cpp' }; $standard = if ($Language -eq 'c') { '-std=c11' } else { '-std=c++17' }
                Invoke-Checked $tool (@($standard,'-Isdk/c',"sdk/$Language/quickstart.$extension") + $objects + @("$CurlRoot/lib/libcurl.dll.a",'-lbcrypt','-ladvapi32','-o',".local/quickstart/native/$target.exe"))
                Invoke-Checked "./.local/quickstart/native/$target.exe" @()
            }
        }
    }
} catch { Write-Error -ErrorAction Continue $_.Exception.Message; exit 2 }
finally { foreach ($name in $saved.Keys) { [Environment]::SetEnvironmentVariable($name,$saved[$name],'Process') }; Pop-Location }
