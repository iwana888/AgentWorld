unit StayCalc;

{$mode objfpc}{$H+}

interface

uses
  SysUtils;

// 计算入住晚数（含首尾）。当前实现为 off-by-one 错误：
// 2026-08-01 ~ 2026-08-03 应返回 3，但返回 2。
function CalculateStayDays(const ACheckIn, ACheckOut: TDateTime): Integer;

implementation

function CalculateStayDays(const ACheckIn, ACheckOut: TDateTime): Integer;
begin
  // BUG: 应使用天数差 + 1（含首尾晚数）
  Result := Trunc(ACheckOut) - Trunc(ACheckIn);
end;

end.
