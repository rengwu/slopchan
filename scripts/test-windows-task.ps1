# Exercise service setup without registering or starting a real scheduled task.
$ErrorActionPreference = 'Stop'
. "$PSScriptRoot/../deploy/setup-windows-task.ps1"
$script:task = $null
$script:registered = 0
$script:started = 0
function Get-ScheduledTask { param($TaskPath, $ErrorAction) $script:task }
function New-ScheduledTaskAction { param($Execute, $Argument) @{ Execute = $Execute; Arguments = $Argument } }
function New-ScheduledTaskTrigger { param([switch]$AtLogOn, $User) @{ User = $User } }
function New-ScheduledTaskPrincipal { param($UserId, $LogonType, $RunLevel) @{ UserId = $UserId } }
function New-ScheduledTaskSettingsSet {
    param($ExecutionTimeLimit, $RestartCount, $RestartInterval, [switch]$AllowStartIfOnBatteries,
          [switch]$DontStopIfGoingOnBatteries, $MultipleInstances)
    @{ RestartCount = $RestartCount }
}
function Register-ScheduledTask {
    param($TaskName, $TaskPath, $Action, $Trigger, $Principal, $Settings, $ErrorAction)
    $script:registered++
    $script:task = @{ TaskName = $TaskName; TaskPath = $TaskPath; Action = $Action; Settings = $Settings }
}
function Start-ScheduledTask { param($TaskName, $TaskPath, $ErrorAction) $script:started++ }
$argsForSetup = @{ TaskName = 'slopchan-test'; Executable = 'C:\Test Folder\slopchan.exe'; DataDirectory = 'C:\Test Folder\data'; TokenFile = 'C:\Test Folder\tokens' }
Start-SlopchanLogonTask @argsForSetup
if ($script:registered -ne 1 -or $script:started -ne 1) { throw 'Fresh task was not registered and started' }
if ($script:task.Action.Arguments -notlike '*"C:\Test Folder\data"*') { throw 'Data path was not quoted' }
$script:task.Action.Arguments += ' -tls-cert "C:\Private TLS\cert.pem" -tls-key "C:\Private TLS\key.pem" -listen 127.0.0.1:8443'
$script:task.Settings.RestartCount = 17
$before = $script:task | ConvertTo-Json -Depth 10 -Compress
Start-SlopchanLogonTask @argsForSetup
if ($script:registered -ne 1 -or $script:started -ne 2) { throw 'Existing task was replaced or not started' }
if (($script:task | ConvertTo-Json -Depth 10 -Compress) -cne $before) { throw 'Existing TLS arguments or settings changed' }
$script:task.Action.Arguments = 'serve -data "C:\Custom Data" -trust-proxy'
$before = $script:task | ConvertTo-Json -Depth 10 -Compress
Start-SlopchanLogonTask @argsForSetup
if (($script:task | ConvertTo-Json -Depth 10 -Compress) -cne $before) { throw 'Existing proxy configuration changed' }
Write-Host 'Windows task creation and configuration preservation passed'
