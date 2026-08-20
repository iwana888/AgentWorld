program test_stringutils;

{$mode objfpc}{$H+}

uses
  SysUtils, StringUtils;

begin
  Assert(SafeTruncate('hello', 10) = 'hello', 'safe truncate over-length should return whole string');
  Assert(SafeTruncate('hello', 3) = 'hel', 'safe truncate should cut to 3');
  Writeln('test_stringutils: PASS');
end.
