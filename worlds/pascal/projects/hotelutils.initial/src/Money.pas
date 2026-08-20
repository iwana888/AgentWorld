unit Money;

{$mode objfpc}{$H+}

interface

uses
  SysUtils, Math;

// 标准四舍五入保留两位小数。当前实现为截断错误：
// RoundMoney(2.345) 应返回 2.35，但返回 2.34。
function RoundMoney(const AValue: Double): Double;

implementation

function RoundMoney(const AValue: Double): Double;
begin
  // BUG: 应使用 Round 而非 Trunc
  Result := Trunc(AValue * 100) / 100;
end;

end.
