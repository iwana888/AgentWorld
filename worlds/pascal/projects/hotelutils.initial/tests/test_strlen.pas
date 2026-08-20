program test_strlen;

{$mode objfpc}{$H+}

uses
  SysUtils, Strlen;

var
  r: Integer;
begin
  r := Len('hello');         // 期望 5
  Assert(r = 5, 'Len("hello") should be 5, got ' + IntToStr(r));
  WriteLn('test_strlen PASS');
end.
