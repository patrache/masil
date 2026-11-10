# masil installer for Windows.
#
#   irm https://raw.githubusercontent.com/patrache/masil/main/scripts/install.ps1 | iex
#
# Environment:
#   MASIL_VERSION        release to install, e.g. 0.1.0 (default: latest)
#   MASIL_INSTALL_DIR    target directory (default: $env:LOCALAPPDATA\Programs\masil)
#   MASIL_DOWNLOAD_BASE  where archives and checksums.txt live (file:// allowed, for local tests)
#   MASIL_INSTALL_LANG   ko | en: message language (default: Korean when the
#                        Windows UI culture is ko-*, else English)
#
# Works on Windows PowerShell 5.1 and PowerShell 7. Needs no administrator
# rights: it writes only under the user's profile and the user's PATH.
# Everything runs inside one script block, so `irm | iex` leaves no variables
# or settings behind in the caller's session.

$masilRunAsFile = [bool]$PSCommandPath
$masilKo = switch ([string][Environment]::GetEnvironmentVariable('MASIL_INSTALL_LANG')) {
	'ko' { $true }
	'en' { $false }
	default { (Get-UICulture).Name -like 'ko*' }
}

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

	# Messages come in pairs, Korean then English, on one line:
	# (T '<ko>' '<en>'), Say-T '<ko>' '<en>', Fail-T '<ko>' '<en>'.
	function T([string]$ko, [string]$en) {
		if ($masilKo) { return $ko }
		return $en
	}
	function Say-T([string]$ko, [string]$en) { Write-Host (T $ko $en) }
	function Fail-T([string]$ko, [string]$en) { Fail (T $ko $en) }

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
			default { Fail-T "지원하지 않는 아키텍처: $name — amd64(x64)·arm64 만 지원합니다" "Unsupported architecture: $name — only amd64 (x64) and arm64 are supported" }
		}
	}

	# Save-Url copies file:// sources directly: PowerShell 7's Invoke-WebRequest
	# has no file scheme. Network downloads get five tries, 2/4/6/8 s apart:
	# GitHub release downloads answered 504 for a while on 2026-11-08.
	# (Invoke-WebRequest -MaximumRetryCount exists only in PowerShell 7.)
	function Save-Url([string]$url, [string]$dest) {
		$hint = (T 'HTTP 오류(404: 레포가 아직 비공개이거나 그 버전이 없음, 5xx: GitHub 쪽 일시 장애)나 네트워크 문제일 수 있습니다 — 잠시 뒤 다시 실행하세요.' 'This may be an HTTP error (404: the repository is still private or that version does not exist, 5xx: a temporary GitHub outage) or a network problem — try again in a moment.')
		$uri = $null
		try {
			$uri = [System.Uri]$url
		} catch {
			Fail-T "내려받기 실패: $url (주소가 올바르지 않습니다)" "Download failed: $url (not a valid address)"
		}
		if ($uri.IsFile) {
			try {
				Copy-Item -LiteralPath $uri.LocalPath -Destination $dest -Force
			} catch {
				Fail-T "내려받기 실패: $url ($($_.Exception.Message))" "Download failed: $url ($($_.Exception.Message))"
			}
			return
		}
		$last = ''
		for ($attempt = 1; $attempt -le 5; $attempt++) {
			try {
				Invoke-WebRequest -Uri $url -OutFile $dest -UseBasicParsing
				return
			} catch {
				# e.g. "The remote server returned an error: (504) Gateway Timeout." (5.1)
				# or "Response status code does not indicate success: 504 (Gateway Timeout)." (7)
				$last = $_.Exception.Message
				if ($attempt -lt 5) { Start-Sleep -Seconds (2 * $attempt) }
			}
		}
		Fail-T "내려받기 실패: $url ($last)`n  $hint" "Download failed: $url ($last)`n  $hint"
	}

	function Test-SameDir([string]$a, [string]$b) {
		return $a.Trim().TrimEnd('\', '/') -ieq $b.Trim().TrimEnd('\', '/')
	}

	try {
		if ($PSVersionTable.PSEdition -eq 'Core' -and -not $IsWindows) {
			Fail-T '이 스크립트는 Windows 전용입니다 — macOS·Linux 는 install.sh 를 쓰세요' 'This script is for Windows only — on macOS and Linux use install.sh'
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
			if (-not $localAppData) { Fail-T 'LOCALAPPDATA 가 비어 있습니다 — MASIL_INSTALL_DIR 로 설치 경로를 정하세요' 'LOCALAPPDATA is empty — set the install folder with MASIL_INSTALL_DIR' }
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
			if (-not $want) { Fail-T "checksums.txt 에 $archive 항목이 없습니다" "checksums.txt has no entry for $archive" }
			$got = (Get-FileHash -LiteralPath $zip -Algorithm SHA256).Hash
			if ($got -ine $want) { Fail-T "체크섬 불일치: $archive" "Checksum mismatch: $archive" }

			$x = Join-Path $tmp 'x'
			try {
				Expand-Archive -LiteralPath $zip -DestinationPath $x -Force
			} catch {
				Fail-T "압축을 풀 수 없습니다: $archive" "Cannot extract $archive"
			}
			$src = Join-Path $x 'masil.exe'
			if (-not (Test-Path -LiteralPath $src -PathType Leaf)) { Fail-T "$archive 안에 masil.exe 가 없습니다" "$archive has no masil.exe" }

			try {
				New-Item -ItemType Directory -Path $dir -Force | Out-Null
			} catch {
				Fail-T "설치 경로를 만들 수 없습니다: $dir" "Cannot create the install folder: $dir"
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
					Fail-T "기존 실행 파일을 옮길 수 없습니다: $target" "Cannot move the existing executable aside: $target"
				}
			}
			try {
				Copy-Item -LiteralPath $src -Destination $target -Force
			} catch {
				if ($aside) { Move-Item -LiteralPath $aside -Destination $target -Force -ErrorAction SilentlyContinue }
				Fail-T "설치 경로에 쓸 수 없습니다: $target" "Cannot write to the install path: $target"
			}
		} finally {
			Remove-Item -LiteralPath $tmp -Recurse -Force -ErrorAction SilentlyContinue
		}

		Say-T "masil 설치 완료: $target ($archive)" "masil installed: $target ($archive)"

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
			Say-T "사용자 PATH 에 $dir 를 추가했습니다 — 새 터미널부터 적용됩니다." "Added $dir to your user PATH — it takes effect in new terminals."
		}

		if ($upgrade) {
			Say-T '실행 중인 알림 데몬은 다음 로그인부터 새 버전으로 뜹니다.' 'A running notification daemon starts on the new version at your next login.'
		} else {
			Say-T '다음 단계:' 'Next steps:'
			Say-T '  masil signup           # 계정 만들기 (이미 있으면 masil login)' '  masil signup           # create an account (or masil login if you have one)'
			Say-T '  masil daemon install   # masil 이 꺼져 있어도 데스크톱 알림 받기' '  masil daemon install   # get desktop notifications even while masil is closed'
		}
		return $true
	} catch {
		if ("$_" -eq $failMarker) { return $false }
		Write-Host (T "masil 설치 실패: $($_.Exception.Message)" "masil install failed: $($_.Exception.Message)") -ForegroundColor Red
		return $false
	}
} | Select-Object -Last 1

if (-not $masilOk) {
	# A `-File` run exits 1. Under `irm | iex` exit would close the user's own
	# shell, so throw instead: the shell stays open, and `pwsh -Command` still
	# ends non-zero.
	if ($masilRunAsFile) { exit 1 }
	$masilFailed = $(if ($masilKo) { 'masil 설치에 실패했습니다' } else { 'masil install failed' })
	Remove-Variable -Name masilOk, masilRunAsFile, masilKo -ErrorAction SilentlyContinue
	try { throw $masilFailed } finally { Remove-Variable -Name masilFailed -ErrorAction SilentlyContinue }
}
Remove-Variable -Name masilOk, masilRunAsFile, masilKo -ErrorAction SilentlyContinue
