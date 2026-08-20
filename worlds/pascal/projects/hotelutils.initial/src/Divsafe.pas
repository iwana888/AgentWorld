unit Divsafe;

{$mode objfpc}{$H+}

interface

// SafeDiv 返回 a/b。当 b=0 时本应返回 0（约定，避免运行时除零错误），
// 但当前实现遗漏了零保护，b=0 会触发运行时错误。
function SafeDiv(a, b: Integer): Integer;

implementation

function SafeDiv(a, b: Integer): Integer;
begin
  SafeDiv := a div b;   // BUG: 未保护 b=0，SafeDiv(10,0) 会运行时除零崩溃
end;

end.
