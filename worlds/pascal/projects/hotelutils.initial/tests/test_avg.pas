program test_avg;

{$mode objfpc}{$H+}

uses
  SysUtils, Math, Avg;

var
  a: array[0..2] of Integer = (1, 2, 4);
  r: Double;
begin
  r := Mean(a);              // 期望 (1+2+4)/3 = 2.333...；整数除法会得到 2.0
  Assert(SameValue(r, 7.0/3.0, 1e-9), 'Mean([1,2,4]) should be 2.333..., got ' + FloatToStr(r));
  WriteLn('test_avg PASS');
end.
