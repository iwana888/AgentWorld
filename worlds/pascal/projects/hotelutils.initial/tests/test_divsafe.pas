program test_divsafe;

{$mode objfpc}{$H+}

uses
  SysUtils, Divsafe;

var
  r: Integer;
begin
  r := SafeDiv(10, 2);      // 期望 5
  Assert(r = 5, 'SafeDiv(10,2) should be 5, got ' + IntToStr(r));
  r := SafeDiv(10, 0);      // 期望 0（不崩溃）
  Assert(r = 0, 'SafeDiv(10,0) should be 0, got ' + IntToStr(r));
  WriteLn('test_divsafe PASS');
end.
