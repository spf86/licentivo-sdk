param(
    [Parameter(Mandatory=$true)][ValidateSet('go','python','javascript','java','csharp','c','cpp','rust','ruby')][string]$Language,
    [string]$Version='0.11.2',
    [string]$Destination='.licentivo-sdk',
    [string]$Project='',
    [string]$PythonExe='python',
    [string]$PackageDirectory='',
    [string]$ReleaseBaseURL='https://www.licentivo.com/assets/sdk/releases',
    [ValidateSet('mingw','msvc')][string]$Toolchain='mingw',
    [switch]$DownloadOnly
)
$ErrorActionPreference='Stop'
if ($Version -notmatch '^\d+\.\d+\.\d+$') { throw 'Use a release version such as 0.11.2.' }
function Checked { param([string]$File,[string[]]$Arguments); & $File @Arguments; if ($LASTEXITCODE -ne 0) { throw "Command failed: $File" } }
# Normal online installation uses public registries. PackageDirectory is reserved
# for release verification and explicitly prepared offline binary packages.
if (!$PackageDirectory -and !$DownloadOnly -and $Language -notin @('c','cpp')) {
    switch ($Language) {
        'go' { Checked 'go' @('get',"github.com/spf86/licentivo-sdk@v$Version") }
        'python' { Checked $PythonExe @('-m','pip','install',"licentivo-sdk==$Version") }
        'javascript' { Checked 'npm.cmd' @('install',"@licentivo/sdk@$Version") }
        'csharp' {
            $arguments=@('add'); if ($Project) { $arguments+= $Project }
            Checked 'dotnet' ($arguments+@('package','Licentivo.SDK','--version',$Version))
        }
        'rust' {
            $arguments=@('add',"licentivo@=$Version"); if ($Project) { $arguments+=@('--manifest-path',$Project) }
            Checked 'cargo' $arguments
        }
        'ruby' { Checked 'ruby' @('-S','gem','install','licentivo-sdk','--version',$Version) }
        'java' { Write-Output "Add to Gradle dependencies (with mavenCentral()): implementation 'com.licentivo:licentivo-sdk:$Version'" }
    }
    return
}
$sdkReleaseBase=[Uri]$ReleaseBaseURL
if (!$sdkReleaseBase.IsAbsoluteUri -or $sdkReleaseBase.Scheme -cne 'https' -or $sdkReleaseBase.UserInfo -or $sdkReleaseBase.Query -or $sdkReleaseBase.Fragment) { throw 'ReleaseBaseURL must be an absolute HTTPS directory URL.' }
$sdkReleaseURL=$ReleaseBaseURL.TrimEnd('/')+"/v$Version"
$sdkInstallRoot=Join-Path (Join-Path $Destination $Language) "v$Version"
New-Item -ItemType Directory -Path $sdkInstallRoot -Force | Out-Null
$sdkInstallRoot=(Resolve-Path -LiteralPath $sdkInstallRoot).Path
if ($PackageDirectory) { $PackageDirectory=(Resolve-Path -LiteralPath $PackageDirectory).Path }
function Fetch { param([string]$Name)
    $target=Join-Path $sdkInstallRoot $Name
    if ($PackageDirectory) {
        $source=Join-Path $PackageDirectory $Name
        if ([IO.Path]::GetFullPath($source) -ne [IO.Path]::GetFullPath($target)) { Copy-Item -LiteralPath $source -Destination $target -Force }
    } else {
        [Net.ServicePointManager]::SecurityProtocol=[Net.SecurityProtocolType]::Tls12
        Invoke-WebRequest -Uri "$sdkReleaseURL/$Name" -OutFile $target -UseBasicParsing
    }
    return $target
}
function Verify { param([string]$Name,[string]$Path)
    $entries=@(Get-Content -LiteralPath (Join-Path $sdkInstallRoot 'SHA256SUMS') | Where-Object { $_.Length -ge 67 -and $_.Substring(66) -ceq $Name })
    if ($entries.Count -ne 1 -or $entries[0] -notmatch '^[a-f0-9]{64}  ') { throw "Missing or ambiguous checksum: $Name" }
    $actual=(Get-FileHash -LiteralPath $Path -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($actual -cne $entries[0].Substring(0,64)) { throw "SHA-256 mismatch: $Name. Installation stopped." }
}
function HasAsset { param([string]$Name)
    return @(Get-Content -LiteralPath (Join-Path $sdkInstallRoot 'SHA256SUMS') | Where-Object { $_ -cmatch '^[a-f0-9]{64}  ' -and $_.Substring(66) -ceq $Name }).Count -eq 1
}
function Unpack { param([string]$Path)
    Add-Type -AssemblyName System.IO.Compression.FileSystem
    $archive=[IO.Compression.ZipFile]::OpenRead($Path)
    try {
        foreach ($entry in $archive.Entries) {
            $name=$entry.FullName.Replace('\','/')
            if ($name.StartsWith('/') -or $name.Contains(':') -or $name -match '(^|/)\.\.(/|$)') { throw 'Unsafe path in SDK archive.' }
        }
    } finally { $archive.Dispose() }
    $output=Join-Path $sdkInstallRoot $(if ($Language -in @('c','cpp') -and $asset.StartsWith('licentivo-native-')) { 'native' } else { 'source' })
    Expand-Archive -LiteralPath $Path -DestinationPath $output -Force
    return $output
}
Fetch 'SHA256SUMS' | Out-Null
$asset=switch ($Language) {
    'python' { "licentivo_sdk-$Version-py3-none-any.whl" }
    'javascript' { "licentivo-sdk-$Version.tgz" }
    'java' { "licentivo-sdk-$Version.jar" }
    'csharp' { "Licentivo.SDK.$Version.nupkg" }
    'ruby' {
        $rubyPlatform=(& ruby -e "require 'rubygems'; print Gem::Platform.local").Trim()
        if ($LASTEXITCODE -ne 0 -or $rubyPlatform -notmatch '^[a-zA-Z0-9_-]+$') { throw 'Cannot determine the installed Ruby platform.' }
        $candidate="licentivo-sdk-$Version-$rubyPlatform.gem"
        if (HasAsset $candidate) { $candidate } else { "licentivo-sdk-$Version.gem" }
    }
    { $_ -in @('c','cpp') } {
        if ($env:OS -eq 'Windows_NT' -and [Environment]::Is64BitOperatingSystem -and $env:PROCESSOR_ARCHITECTURE -in @('AMD64','x86')) {
            $candidate="licentivo-native-$Version-windows-x64-$Toolchain.zip"
        } else { $candidate='' }
        if ($candidate -and (HasAsset $candidate)) { $candidate } else { throw 'No verified native package matches this platform/compiler. See the supported platforms in the SDK installation guide.' }
    }
    default { "licentivo-$Language-$Version.zip" }
}
$package=Fetch $asset
Verify $asset $package
Write-Output "Downloaded and verified $asset"
if ($Language -in @('go','c','cpp','rust')) {
    $source=Unpack $package
    Write-Output "SDK source: $source"
}
if ($DownloadOnly) { Write-Output 'Download complete; no package manager was run.'; return }
switch ($Language) {
    'rust' {
        if (!$Project) { $Project='Cargo.toml' }
        if (!(Test-Path -LiteralPath $Project -PathType Leaf) -or (Split-Path -Leaf $Project) -cne 'Cargo.toml') { throw 'Run inside your Cargo project, or pass -Project path/to/Cargo.toml. Use -DownloadOnly to fetch only.' }
        Checked 'cargo' @('add','--manifest-path',(Resolve-Path -LiteralPath $Project).Path,'--path',"$source/sdk/rust")
        Write-Output 'Import: use licentivo::Client; Cargo builds the C core. See docs/install.md for native prerequisites.'
    }
    'ruby' {
        Checked 'ruby' @('-S','gem','install','--local',$package,'--no-document','--install-dir',(Join-Path $sdkInstallRoot 'gems'))
        Write-Output "Ruby package installed. In this terminal run: `$env:GEM_PATH='$sdkInstallRoot/gems;'+(& ruby -e 'print Gem.path.join(File::PATH_SEPARATOR)')"
        Write-Output "Import: require 'licentivo'. Keep this directory with your application."
    }
    'go' {
        if (!(Test-Path -LiteralPath 'go.mod')) { throw 'Run this installer inside your Go module (go mod init first), or use -DownloadOnly.' }
        Checked 'go' @('mod','edit',"-require=github.com/spf86/licentivo-sdk@v$Version")
        Checked 'go' @('mod','edit',"-replace=github.com/spf86/licentivo-sdk=$source")
        Write-Output 'Import: github.com/spf86/licentivo-sdk'
        Write-Output 'The verified standalone module is a project-local dependency. Keep .licentivo-sdk with your application.'
    }
    'python' {
        Checked $PythonExe @('-m','pip','install',$package)
        Write-Output 'Import: from licentivo import Client'
    }
    'javascript' {
        if (!(Test-Path -LiteralPath 'package.json')) { throw 'Run npm init in your application first, or use -DownloadOnly.' }
        $npmCommand=if ($IsWindows -or $env:OS -eq 'Windows_NT') { 'npm.cmd' } else { 'npm' }
        Checked $npmCommand @('install',$package)
        Write-Output "Import: import {Client} from '@licentivo/sdk'"
    }
    'java' {
        Write-Output "Add this JAR to your classpath or Gradle dependencies: $package"
    }
    'csharp' {
        if (!$Project) {
            $projects=@(Get-ChildItem -LiteralPath '.' -Filter '*.csproj' -File)
            if ($projects.Count -eq 1) { $Project=$projects[0].FullName }
        }
        if (!$Project) { Write-Output 'Package ready. Rerun with -Project your-app.csproj.'; return }
        $Project=(Resolve-Path -LiteralPath $Project).Path
        if (![string]::Equals([IO.Path]::GetExtension($Project),'.csproj',[StringComparison]::OrdinalIgnoreCase)) { throw 'Project must name a .csproj file.' }
        $projectFolder=Split-Path -Parent $Project
        $nugetConfigs=@(Get-ChildItem -LiteralPath $projectFolder -File | Where-Object { $_.Name -ieq 'NuGet.Config' })
        if ($nugetConfigs.Count -gt 1) { throw 'Multiple project NuGet configurations; consolidate them before installation.' }
        $nugetConfig=if ($nugetConfigs.Count -eq 1) { $nugetConfigs[0].FullName } else { Join-Path $projectFolder 'NuGet.Config' }
        if (!(Test-Path -LiteralPath $nugetConfig)) { [IO.File]::WriteAllText($nugetConfig,'<configuration><packageSources /></configuration>',[Text.UTF8Encoding]::new($false)) }
        $nugetDocument=[xml](Get-Content -LiteralPath $nugetConfig -Raw)
        if (!$nugetDocument.configuration) { throw 'Invalid project NuGet configuration.' }
        $sources=$nugetDocument.configuration.SelectSingleNode('packageSources')
        if (!$sources) { $sources=$nugetDocument.CreateElement('packageSources'); $nugetDocument.configuration.AppendChild($sources) | Out-Null }
        $feedName="licentivo-release-$Version"
        $existing=$sources.SelectNodes('add') | Where-Object { $_.GetAttribute('key') -ceq $feedName }
        foreach ($entry in $existing) { $sources.RemoveChild($entry) | Out-Null }
        $sourceEntry=$nugetDocument.CreateElement('add')
        $sourceEntry.SetAttribute('key',$feedName); $sourceEntry.SetAttribute('value',$sdkInstallRoot)
        $sources.AppendChild($sourceEntry) | Out-Null
        $nugetDocument.Save($nugetConfig)
        Checked 'dotnet' @('add',$Project,'package','Licentivo.SDK','--version',$Version,'--no-restore')
        Checked 'dotnet' @('restore',$Project)
        Write-Output 'Import: using Licentivo;'
    }
    { $_ -in @('c','cpp') } {
        if ($asset.StartsWith('licentivo-native-')) {
            Write-Output "CMake prefix: $source. Configure with -DCMAKE_PREFIX_PATH=$source, then find_package(Licentivo $Version REQUIRED) and link Licentivo::C or Licentivo::CPP."
        } else { Write-Output "Source package: add_subdirectory($source/sdk licentivo-build), then link Licentivo::C or Licentivo::CPP." }
        Write-Output 'Use matching compiler/ABI and libcurl dependencies. See docs/install.md.'
    }
}
