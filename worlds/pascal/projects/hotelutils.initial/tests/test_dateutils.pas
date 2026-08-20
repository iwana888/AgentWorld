program test_dateutils;

{$mode objfpc}{$H+}

uses
  SysUtils, StayCalc;

var
  d1, d2: TDateTime;
begin
  d1 := EncodeDate(2026, 8, 1);
  d2 := EncodeDate(2026, 8, 3);
  Assert(CalculateStayDays(d1, d2) = 3, 'stay days should be 3, got ' + IntToStr(CalculateStayDays(d1, d2)));
  Writeln('test_dateutils: PASS');
end.
