# Installs the latest lazyforge release for Windows. Meant for: irm <url>/install.ps1 | iex
# Under `irm | iex` this runs in the caller's session, so the preferences changed here are put back at the end.
$oldErrorAction = $ErrorActionPreference
$oldProgress = $ProgressPreference
$ErrorActionPreference = 'Stop'
# Windows PowerShell 5.1 renders download progress so slowly that it dominates the transfer time.
$ProgressPreference = 'SilentlyContinue'
[Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12

$base = $env:LAZYFORGE_BASE_URL
if (-not $base) { $base = 'https://git.bobparsons.dev' }
$base = $base.TrimEnd('/')

$dir = $env:LAZYFORGE_INSTALL_DIR
if (-not $dir) { $dir = Join-Path $env:LOCALAPPDATA 'Programs\lazyforge' }

# A 32-bit PowerShell on 64-bit Windows reports the native architecture only in PROCESSOR_ARCHITEW6432.
$machine = $env:PROCESSOR_ARCHITEW6432
if (-not $machine) { $machine = $env:PROCESSOR_ARCHITECTURE }
switch ($machine) {
  'AMD64' { $arch = 'amd64' }
  'ARM64' { $arch = 'arm64' }
  default { throw "unsupported architecture: $machine" }
}

$tag = $env:LAZYFORGE_VERSION
if (-not $tag) {
  $tag = (Invoke-RestMethod -Uri "$base/api/v1/repos/deadstyle/lazyforge/releases/latest" -UseBasicParsing).tag_name
  if (-not $tag) { throw 'latest release has no tag' }
}

$zipName = "lazyforge_${tag}_windows_${arch}.zip"
$release = "$base/deadstyle/lazyforge/releases/download/$tag"
$tmp = Join-Path ([IO.Path]::GetTempPath()) ('lazyforge-' + [Guid]::NewGuid())
New-Item -ItemType Directory -Path $tmp | Out-Null
try {
  $zip = Join-Path $tmp $zipName
  $sums = Join-Path $tmp 'checksums.txt'
  Invoke-WebRequest -Uri "$release/$zipName" -OutFile $zip -UseBasicParsing
  Invoke-WebRequest -Uri "$release/checksums.txt" -OutFile $sums -UseBasicParsing

  $want = $null
  foreach ($line in Get-Content -LiteralPath $sums) {
    $f = $line.Trim() -split '\s+'
    if ($f.Count -eq 2 -and $f[1].TrimStart('*') -ceq $zipName) { $want = $f[0] }
  }
  if (-not $want) { throw "no checksum for $zipName" }
  $got = (Get-FileHash -Algorithm SHA256 -LiteralPath $zip).Hash
  if ($got -ine $want) { throw "checksum mismatch for $zipName" }

  $unpacked = Join-Path $tmp 'unpacked'
  Expand-Archive -LiteralPath $zip -DestinationPath $unpacked
  $newExe = Join-Path $unpacked 'lazyforge.exe'
  if (-not (Test-Path -LiteralPath $newExe)) { throw 'archive has no lazyforge.exe' }

  New-Item -ItemType Directory -Force -Path $dir | Out-Null
  $exe = Join-Path $dir 'lazyforge.exe'
  try {
    Copy-Item -LiteralPath $newExe -Destination $exe -Force -ErrorAction Stop
  } catch {
    # Windows refuses to overwrite a running exe but lets it be renamed.
    Remove-Item -LiteralPath "$exe.old" -Force -ErrorAction SilentlyContinue
    Move-Item -LiteralPath $exe -Destination "$exe.old" -Force
    Copy-Item -LiteralPath $newExe -Destination $exe -Force
  }

  # Read and write the raw value: GetEnvironmentVariable expands %VARS% and would freeze them into the stored PATH.
  $envKey = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey('Environment', $true)
  $userPath = [string]$envKey.GetValue('Path', '', [Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames)
  $entries = @($userPath -split ';' | Where-Object { $_ })
  if (-not ($entries | Where-Object { $_.TrimEnd('\') -ieq $dir.TrimEnd('\') })) {
    $entries += $dir
    $envKey.SetValue('Path', ($entries -join ';'), [Microsoft.Win32.RegistryValueKind]::ExpandString)
    $pathChanged = $true
  }
  $envKey.Close()
  if ($pathChanged) {
    # Registry writes alone don't tell running programs; setting and clearing a user variable makes Windows broadcast the change, so new terminals see the PATH.
    [Environment]::SetEnvironmentVariable('LAZYFORGE_PATH_REFRESH', '1', 'User')
    [Environment]::SetEnvironmentVariable('LAZYFORGE_PATH_REFRESH', $null, 'User')
  }
  if (-not (($env:Path -split ';') | Where-Object { $_.TrimEnd('\') -ieq $dir.TrimEnd('\') })) {
    $env:Path = "$env:Path;$dir"
  }

  Write-Output "installed lazyforge $tag to $exe"
} finally {
  Remove-Item -LiteralPath $tmp -Recurse -Force -ErrorAction SilentlyContinue
  $ProgressPreference = $oldProgress
  $ErrorActionPreference = $oldErrorAction
}
