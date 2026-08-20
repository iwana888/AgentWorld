program test_calc;

{$mode objfpc}{$H+}

uses
  SysUtils, Calc;

var
  r: Integer;
begin
  r := SumTo(5);             // 期望 1+2+3+4+5 = 15
  Assert(r = 15, 'SumTo(5) should be 15, got ' + IntToStr(r));
  r := SumTo(1);             // 期望 1
  Assert(r = 1, 'SumTo(1) should be 1, got ' + IntToStr(r));
  WriteLn('test_calc PASS');
end.
