unit StringUtils;

{$mode objfpc}{$H+}

interface

uses
  SysUtils;

// 安全截断字符串到 n 个字符。当 n 大于字符串长度时，应原样返回整个字符串。
// 当前实现在 n > Length(s) 时错误地返回空串。
function SafeTruncate(const AStr: string; ALen: Integer): string;

implementation

function SafeTruncate(const AStr: string; ALen: Integer): string;
begin
  // BUG: 当请求长度超过字符串长度时错误地返回空
  if ALen > Length(AStr) then
    Result := ''
  else if ALen <= 0 then
    Result := ''
  else
    Result := Copy(AStr, 1, ALen);
end;

end.
