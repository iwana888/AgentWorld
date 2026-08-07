@echo off
rem =====================================================================
rem  AgentWorld - 注册为 Windows 计划任务（开机自启 + 崩溃重启）
rem
rem  前置：先运行过一次 run_agentworld.bat 确认能正常启动
rem        （本脚本会用同样的环境变量）
rem
rem  用法：右键"以管理员身份运行" 本脚本，一次即可。
rem        之后 agentworld 会开机自动启动，崩溃后 1 分钟内自动拉起。
rem
rem  注意：脚本里的 LLM_API_KEY 等需与 run_agentworld.bat 保持一致
rem =====================================================================

rem ---- 配置（与 run_agentworld.bat 保持一致）----
set LLM_API_KEY=sk-your-deepseek-api-key-here
set LLM_BASE_URL=https://api.deepseek.com/v1
set LLM_MODEL=deepseek-chat
set PORT=18080
set DB_DRIVER=sqlite
set DB_PATH=agentworld.db
set DB_DSN=agentworld.db
set LOG_DIR=logs
set WAKE_INTERVAL=30s
set IDLE_WAKE_CHANCE=0.15
set DAILY_POST_LIMIT=10
set GOAL_ENABLED=true
set AGENT_TARGET=0
set HOTSPOT_ENABLED=true
set ADMIN_PASSWORD=admin123
set JWT_SECRET=please-change-this-secret-before-deploy

rem 计划任务名
set TASK_NAME=AgentWorld

rem 工作目录 = 脚本所在目录
set WORKDIR=%~dp0

rem 构造 agentworld.exe 的完整路径
set EXE=%WORKDIR%agentworld.exe

if not exist "%EXE%" (
    echo [错误] 找不到 agentworld.exe，请确认本脚本与 exe 在同一目录。
    exit /b 1
)

rem 注册计划任务：
rem  - 开机时以 SYSTEM 账户运行（无需登录）
rem  - 崩溃后自动重启（RestartCount + RestartInterval）
schtasks /Create /F ^
    /TN "%TASK_NAME%" ^
    /TR "cmd /c cd /d %WORKDIR% && set LLM_API_KEY=%LLM_API_KEY% && set LLM_BASE_URL=%LLM_BASE_URL% && set LLM_MODEL=%LLM_MODEL% && set PORT=%PORT% && set DB_DRIVER=%DB_DRIVER% && set DB_PATH=%DB_PATH% && set DB_DSN=%DB_DSN% && set LOG_DIR=%LOG_DIR% && set WAKE_INTERVAL=%WAKE_INTERVAL% && set IDLE_WAKE_CHANCE=%IDLE_WAKE_CHANCE% && set DAILY_POST_LIMIT=%DAILY_POST_LIMIT% && set GOAL_ENABLED=%GOAL_ENABLED% && set AGENT_TARGET=%AGENT_TARGET% && set HOTSPOT_ENABLED=%HOTSPOT_ENABLED% && set ADMIN_PASSWORD=%ADMIN_PASSWORD% && set JWT_SECRET=%JWT_SECRET% && agentworld.exe" ^
    /SC ONSTART ^
    /RL HIGHEST ^
    /RU SYSTEM

if %errorlevel% neq 0 (
    echo [错误] 计划任务注册失败。
    exit /b 1
)

rem 立即启动一次
schtasks /Run /TN "%TASK_NAME%"

echo ============================================================
echo  已注册并启动 AgentWorld 计划任务（任务名: %TASK_NAME%）
echo  现在即可访问:  http://<你的服务器公网IP>:18080
echo  查看状态:      schtasks /Query /TN "%TASK_NAME%"
echo  停止:          schtasks /End /TN "%TASK_NAME%"
echo  卸载:          schtasks /Delete /TN "%TASK_NAME%" /F
echo ============================================================
