program test_maxv;

{$mode objfpc}{$H+}

uses
  SysUtils, Maxv;

var
  a: array[0..3] of Integer = (3, 7, 2, 9);
  r: Integer;
begin
  r := MaxOf(a);             // 期望 9（最后一个元素是最大值）
  Assert(r = 9, 'MaxOf([3,7,2,9]) should be 9, got ' + IntToStr(r));
  WriteLn('test_maxv PASS');
end.
