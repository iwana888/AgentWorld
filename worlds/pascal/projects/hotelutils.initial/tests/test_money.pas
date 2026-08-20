program test_money;

{$mode objfpc}{$H+}

uses
  SysUtils, Math, Money;

begin
  Assert(SameValue(2.35, RoundMoney(2.345), 1e-9), 'round money should be 2.35');
  Writeln('test_money: PASS');
end.
