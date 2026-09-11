# Loaded by install.ps1. Existing task actions and settings belong to the owner.
function Start-SlopchanLogonTask {
    [CmdletBinding()]
    param(
        [Parameter(Mandatory)][string]$TaskName,
        [Parameter(Mandatory)][string]$Executable,
        [Parameter(Mandatory)][string]$DataDirectory,
        [Parameter(Mandatory)][string]$TokenFile
    )
    # Enumerating tasks distinguishes absence from permission failures.
    $existing = Get-ScheduledTask -ErrorAction Stop |
        Where-Object { $_.TaskName -eq $TaskName -and $_.TaskPath -eq '\' }
    if (!$existing) {
        $user = [Security.Principal.WindowsIdentity]::GetCurrent().Name
        $action = New-ScheduledTaskAction -Execute $Executable -Argument "serve -data `"$DataDirectory`" -token-file `"$TokenFile`""
        $trigger = New-ScheduledTaskTrigger -AtLogOn -User $user
        $principal = New-ScheduledTaskPrincipal -UserId $user -LogonType Interactive -RunLevel Limited
        $settings = New-ScheduledTaskSettingsSet -ExecutionTimeLimit ([TimeSpan]::Zero) -RestartCount 999 -RestartInterval (New-TimeSpan -Minutes 1) -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries -MultipleInstances IgnoreNew
        Register-ScheduledTask -TaskName $TaskName -TaskPath '\' -Action $action -Trigger $trigger -Principal $principal -Settings $settings -ErrorAction Stop | Out-Null
    }
    Start-ScheduledTask -TaskName $TaskName -TaskPath '\' -ErrorAction Stop
    Write-Host 'Scheduled task started; existing launch arguments and settings are preserved.'
}
