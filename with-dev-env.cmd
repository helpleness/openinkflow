@echo off
call "%~dp0inkflow-env.cmd" activate --quiet
if errorlevel 1 exit /b %errorlevel%
%*
