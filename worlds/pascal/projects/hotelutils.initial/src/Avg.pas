unit Avg;

{$mode objfpc}{$H+}

interface

// Mean 返回整数数组的平均值（浮点）。
function Mean(const a: array of Integer): Double;

implementation

function Mean(const a: array of Integer): Double;
var
  i: Integer;
  s: Integer;
begin
  s := 0;
  for i := 0 to High(a) do
    s := s + a[i];
  Mean := s div Length(a);   // BUG: 整数除法，丢失小数
end;

end.
