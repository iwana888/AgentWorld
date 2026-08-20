unit Strlen;

{$mode objfpc}{$H+}

interface

// Len 返回字符串长度（教学实现，不调用系统 Length）。
function Len(const s: string): Integer;

implementation

function Len(const s: string): Integer;
var
  i: Integer;
begin
  i := 0;
  while s[i] <> #0 do        // BUG: Pascal 字符串不是 #0 结尾，索引从 1 开始，会越界
    i := i + 1;
  Len := i;
end;

end.
