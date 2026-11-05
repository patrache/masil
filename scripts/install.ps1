# masil installer for Windows.
#
#   irm https://raw.githubusercontent.com/patrache/masil/main/scripts/install.ps1 | iex
#
# Environment:
#   MASIL_VERSION        release to install, e.g. 0.1.0 (default: latest)
#   MASIL_INSTALL_DIR    target directory (default: $env:LOCALAPPDATA\Programs\masil)
#   MASIL_DOWNLOAD_BASE  where archives and checksums.txt live (file:// allowed, for local tests)
#
# Works on Windows PowerShell 5.1 and PowerShell 7. Needs no administrator
# rights: it writes only under the user's profile and the user's PATH.
# Everything runs inside one script block, so `irm | iex` leaves no variables
# or settings behind in the caller's session.

$masilRunAsFile = [bool]$PSCommandPath

$masilOk = & {
	Set-StrictMode -Version 3.0
	$ErrorActionPreference = 'Stop'
	$ProgressPreference = 'SilentlyContinue'

	$repoUrl = 'https://github.com/patrache/masil'

	$failMarker = 'masil-install-failed'
	function Fail([string]$msg) {
		Write-Host $msg -ForegroundColor Red
		throw $failMarker
	}

	# Env reads go through .NET so unset variables are plain $null under StrictMode.
	function Get-Env([string]$name) {
		return [Environment]::GetEnvironmentVariable($name)
	}

	function Get-MasilArch {
		$name = ''
		try {
			$name = [string][System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture
		} catch {
			# Older .NET Framework without RuntimeInformation: ask the OS instead.
			$name = Get-Env 'PROCESSOR_ARCHITEW6432'
			if (-not $name) { $name = Get-Env 'PROCESSOR_ARCHITECTURE' }
		}
		switch -Regex ($name) {
			'^(x64|amd64)$' { return 'amd64' }
			'^arm64$' { return 'arm64' }
			default { Fail "지원하지 않는 아키텍처: $name — amd64(x64)·arm64 만 지원합니다" }
		}
	}

	# Save-Url copies file:// sources directly: PowerShell 7's Invoke-WebRequest
	# has no file scheme.
	function Save-Url([string]$url, [string]$dest) {
		try {
			$uri = [System.Uri]$url
			if ($uri.IsFile) {
				Copy-Item -LiteralPath $uri.LocalPath -Destination $dest -Force
			} else {
				Invoke-WebRequest -Uri $url -OutFile $dest -UseBasicParsing
			}
		} catch {
			Fail "내려받기 실패: $url (레포가 아직 비공개이거나 버전이 없습니다)"
		}
	}

	function Test-SameDir([string]$a, [string]$b) {
		return $a.Trim().TrimEnd('\', '/') -ieq $b.Trim().TrimEnd('\', '/')
	}

	try {
		if ($PSVersionTable.PSEdition -eq 'Core' -and -not $IsWindows) {
			Fail '이 스크립트는 Windows 전용입니다 — macOS·Linux 는 install.sh 를 쓰세요'
		}

		# Windows PowerShell 5.1 may still default to TLS 1.0/1.1; GitHub needs 1.2.
		try {
			[Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12
		} catch {}

		$arch = Get-MasilArch
		$archive = "masil_windows_$arch.zip"

		$version = [string](Get-Env 'MASIL_VERSION')
		$version = $version.Trim()
		if ($version.StartsWith('v')) { $version = $version.Substring(1) }
		$downloadBase = Get-Env 'MASIL_DOWNLOAD_BASE'
		if ($downloadBase) {
			$base = $downloadBase.TrimEnd('/')
		} elseif ($version) {
			$base = "$repoUrl/releases/download/v$version"
		} else {
			$base = "$repoUrl/releases/latest/download"
		}

		$dir = Get-Env 'MASIL_INSTALL_DIR'
		if (-not $dir) {
			$localAppData = Get-Env 'LOCALAPPDATA'
			if (-not $localAppData) { Fail 'LOCALAPPDATA 가 비어 있습니다 — MASIL_INSTALL_DIR 로 설치 경로를 정하세요' }
			$dir = Join-Path $localAppData 'Programs\masil'
		}
		$target = Join-Path $dir 'masil.exe'

		$tmp = Join-Path ([System.IO.Path]::GetTempPath()) ('masil-install-' + [System.Guid]::NewGuid().ToString('N'))
		New-Item -ItemType Directory -Path $tmp -Force | Out-Null
		try {
			$zip = Join-Path $tmp $archive
			$sums = Join-Path $tmp 'checksums.txt'
			Save-Url "$base/$archive" $zip
			Save-Url "$base/checksums.txt" $sums

			$want = $null
			foreach ($line in Get-Content -LiteralPath $sums) {
				$parts = $line.Trim() -split '\s+'
				if ($parts.Count -ge 2 -and ($parts[1] -eq $archive -or $parts[1] -eq "*$archive")) {
					$want = $parts[0]
					break
				}
			}
			if (-not $want) { Fail "checksums.txt 에 $archive 항목이 없습니다" }
			$got = (Get-FileHash -LiteralPath $zip -Algorithm SHA256).Hash
			if ($got -ine $want) { Fail "체크섬 불일치: $archive" }

			$x = Join-Path $tmp 'x'
			try {
				Expand-Archive -LiteralPath $zip -DestinationPath $x -Force
			} catch {
				Fail "압축을 풀 수 없습니다: $archive"
			}
			$src = Join-Path $x 'masil.exe'
			if (-not (Test-Path -LiteralPath $src -PathType Leaf)) { Fail "$archive 안에 masil.exe 가 없습니다" }

			try {
				New-Item -ItemType Directory -Path $dir -Force | Out-Null
			} catch {
				Fail "설치 경로를 만들 수 없습니다: $dir"
			}

			# A running masil.exe (the notification daemon) cannot be overwritten,
			# but it can be renamed. Move it aside first, then copy the new one in.
			$upgrade = Test-Path -LiteralPath $target -PathType Leaf
			$aside = $null
			if ($upgrade) {
				Get-ChildItem -LiteralPath $dir -Filter 'masil.exe*.old' -File -ErrorAction SilentlyContinue |
					ForEach-Object { Remove-Item -LiteralPath $_.FullName -Force -ErrorAction SilentlyContinue }
				$aside = "$target.old"
				if (Test-Path -LiteralPath $aside) {
					# The previous .old is still in use (an older daemon): leave it.
					$aside = "$target." + [System.Guid]::NewGuid().ToString('N').Substring(0, 8) + '.old'
				}
				try {
					Move-Item -LiteralPath $target -Destination $aside -Force
				} catch {
					Fail "기존 실행 파일을 옮길 수 없습니다: $target"
				}
			}
			try {
				Copy-Item -LiteralPath $src -Destination $target -Force
			} catch {
				if ($aside) { Move-Item -LiteralPath $aside -Destination $target -Force -ErrorAction SilentlyContinue }
				Fail "설치 경로에 쓸 수 없습니다: $target"
			}
		} finally {
			Remove-Item -LiteralPath $tmp -Recurse -Force -ErrorAction SilentlyContinue
		}

		Write-Host "masil 설치 완료: $target ($archive)"

		$userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
		if (-not $userPath) { $userPath = '' }
		$inUserPath = $false
		foreach ($p in $userPath.Split(';')) {
			if ($p -and (Test-SameDir $p $dir)) { $inUserPath = $true; break }
		}
		if (-not $inUserPath) {
			$newPath = if ($userPath.Trim()) { $userPath.TrimEnd(';') + ';' + $dir } else { $dir }
			[Environment]::SetEnvironmentVariable('Path', $newPath, 'User')
			$env:Path = ([string](Get-Env 'Path')).TrimEnd(';') + ';' + $dir
			Write-Host "사용자 PATH 에 $dir 를 추가했습니다 — 새 터미널부터 적용됩니다."
		}

		if ($upgrade) {
			Write-Host '실행 중인 알림 데몬은 다음 로그인부터 새 버전으로 뜹니다.'
		} else {
			Write-Host '다음 단계:'
			Write-Host '  masil signup           # 계정 만들기 (이미 있으면 masil login)'
			Write-Host '  masil daemon install   # masil 이 꺼져 있어도 데스크톱 알림 받기'
		}
		return $true
	} catch {
		if ("$_" -eq $failMarker) { return $false }
		Write-Host "masil 설치 실패: $($_.Exception.Message)" -ForegroundColor Red
		return $false
	}
} | Select-Object -Last 1

if (-not $masilOk) {
	# A `-File` run exits 1. Under `irm | iex` exit would close the user's own
	# shell, so throw instead: the shell stays open, and `pwsh -Command` still
	# ends non-zero.
	if ($masilRunAsFile) { exit 1 }
	Remove-Variable -Name masilOk, masilRunAsFile -ErrorAction SilentlyContinue
	throw 'masil 설치에 실패했습니다'
}
Remove-Variable -Name masilOk, masilRunAsFile -ErrorAction SilentlyContinue
