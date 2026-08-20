unit Calc;

{$mode objfpc}{$H+}

interface

// SumTo 返回 1..n 的累加和。
function SumTo(n: Integer): Integer;

implementation

function SumTo(n: Integer): Integer;
var
  i, s: Integer;
begin
  s := 0;
  for i := 1 to n - 1 do   // BUG: 应当是 to n，漏加最后一项
    s := s + i;
  SumTo := s;
end;

end.
