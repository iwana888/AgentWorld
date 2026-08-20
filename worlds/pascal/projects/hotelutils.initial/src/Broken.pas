unit Broken;

{$mode objfpc}{$H+}

interface

uses
  SysUtils;

// 该单元用于 Issue #005：当前存在未声明标识符，导致整个工程编译失败。
// 修复 #005 即移除对 UndefinedSymbol 的非法引用。
function BrokenFunc: Integer;

implementation

function BrokenFunc: Integer;
begin
  // BUG: UndefinedSymbol 未声明，编译错误
  Result := UndefinedSymbol + 1;
end;

end.
