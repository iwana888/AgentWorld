program test_guest;

{$mode objfpc}{$H+}

uses
  SysUtils, Guest;

begin
  Assert(GetGuestName('Alice') = 'Alice', 'guest name should be Alice, got empty');
  Writeln('test_guest: PASS');
end.
