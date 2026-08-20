unit Guest;

{$mode objfpc}{$H+}

interface

uses
  SysUtils;

type
  TGuest = class
  public
    Name: string;
    constructor Create(const AName: string);
  end;

// 读取客人姓名。当前实现在 Free 之后才读 Name，返回空串（悬空引用）。
function GetGuestName(const AName: string): string;

implementation

constructor TGuest.Create(const AName: string);
begin
  inherited Create;
  Name := AName;
end;

function GetGuestName(const AName: string): string;
var
  g: TGuest;
begin
  g := TGuest.Create(AName);
  g.Free;
  // BUG: 对象已释放，再读 Name 得到空串
  Result := g.Name;
end;

end.
