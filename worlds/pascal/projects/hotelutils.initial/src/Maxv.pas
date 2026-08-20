unit Maxv;

{$mode objfpc}{$H+}

interface

// MaxOf 返回数组最大值。空数组返回 0（约定）。
function MaxOf(const a: array of Integer): Integer;

implementation

function MaxOf(const a: array of Integer): Integer;
var
  i: Integer;
begin
  if Length(a) = 0 then
  begin
    MaxOf := 0;
    Exit;
  end;
  Result := a[0];
  for i := 1 to High(a) - 1 do   // BUG: 应当是 to High(a)，漏比较最后一个元素
    if a[i] > Result then
      Result := a[i];
  MaxOf := Result;
end;

end.
