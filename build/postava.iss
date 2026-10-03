; goCOP Postava — instalacijski program za Windows (Inno Setup 6)
;
; Ne nosi goCOP u sebi: instalira samo Postavu, a ona pri instalaciji s
; GitHuba preuzme najnovije potpisano izdanje goCOP-a (docs/plan-instalacija.md).
; Zato se gradi rijetko, uz novu Postavu (oznaka postava-v*), a ne uz svako
; izdanje goCOP-a. Bez interneta: uz instalacijski program stavite
; gocop-windows-amd64.exe, SHA256SUMS i SHA256SUMS.sig iz izdanja, pa ih
; Postava uzme odande, uz istu provjeru potpisa.
;
; Datoteka je UTF-8 s BOM-om, da Inno Setup ispravno pročita hrvatska slova.
; Gradi CI (.github/workflows/postava.yml):
;   iscc /DVerzija=1.0.0 build\postava.iss

#ifndef Verzija
  #define Verzija "0.0.0"
#endif

[Setup]
AppId={{6F1D2C7A-3B4E-4C1F-9A2E-60C0B0FA0C01}
AppName=goCOP
AppVersion={#Verzija}
AppVerName=goCOP (Postava {#Verzija})
AppPublisherURL=https://github.com/tkraljevic/goCOP
AppSupportURL=https://github.com/tkraljevic/goCOP/issues
AppUpdatesURL=https://github.com/tkraljevic/goCOP/releases
DefaultDirName={localappdata}\goCOP
DisableProgramGroupPage=yes
PrivilegesRequired=lowest
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
MinVersion=10.0
LicenseFile=licenca.txt
SetupIconFile=gocop.ico
UninstallDisplayIcon={app}\postava\gocop-postava.exe
UninstallDisplayName=goCOP
OutputDir=..\dist
OutputBaseFilename=goCOP-postava-{#Verzija}
Compression=lzma2
SolidCompression=yes
WizardStyle=modern
CloseApplications=no
ShowLanguageDialog=no
VersionInfoProductName=goCOP Postava
VersionInfoVersion={#Verzija}
VersionInfoCopyright=© Hrvatske vode · EUPL-1.2

[Messages]
SetupAppTitle=goCOP — instalacija
SetupWindowTitle=goCOP — instalacija
ButtonBack=< &Natrag
ButtonNext=&Dalje >
ButtonInstall=&Instaliraj
ButtonOK=U redu
ButtonCancel=Odustani
ButtonYes=&Da
ButtonNo=&Ne
ButtonFinish=&Završi
ButtonBrowse=&Odaberi…
ExitSetupTitle=Prekid instalacije
ExitSetupMessage=Instalacija nije gotova. Ako sada izađete, goCOP neće biti instaliran.%n%nInstalaciju možete ponovno pokrenuti kasnije.%n%nPrekinuti instalaciju?
WelcomeLabel1=Dobrodošli u instalaciju goCOP-a
WelcomeLabel2=Instalira se goCOP Postava {#Verzija} za ovog korisnika Windowsa, bez administratorskih prava.%n%nPostava će zatim s GitHuba preuzeti najnovije izdanje goCOP-a, provjeriti mu potpis i pokrenuti čvor. Ikona će biti u traci uz sat, a preglednik otvara postavljanje: nova mreža ili povezivanje s postojećom.
WizardLicense=Licenca
LicenseLabel=Pročitajte uvjete prije nastavka.
LicenseLabel3=goCOP je otvoreni program pod Javnom licencijom Europske unije (EUPL-1.2).
LicenseAccepted=&Prihvaćam uvjete licence
LicenseNotAccepted=&Ne prihvaćam
WizardSelectDir=Gdje instalirati
SelectDirDesc=U koju mapu instalirati goCOP?
SelectDirLabel3=goCOP se instalira u ovu mapu. U njoj nastaju i podmape program, postava i data (baza, postavke, dnevnici).
SelectDirBrowseLabel=Za nastavak kliknite Dalje. Za drugu mapu kliknite Odaberi.
WizardSelectTasks=Dodatne mogućnosti
SelectTasksDesc=Što još napraviti?
SelectTasksLabel2=Izaberite i kliknite Dalje.
WizardReady=Spremno za instalaciju
ReadyLabel1=goCOP je spreman za instalaciju.
ReadyLabel2a=Kliknite Instaliraj za nastavak ili Natrag za izmjenu.
ReadyMemoDir=Mapa:
ReadyMemoTasks=Dodatne mogućnosti:
WizardInstalling=Instalacija
InstallingLabel=Instaliram goCOP i preuzimam najnovije izdanje. To može potrajati minutu.
FinishedHeadingLabel=goCOP je instaliran
FinishedLabelNoIcons=goCOP je instaliran.
FinishedLabel=goCOP je instaliran. Ikona je u traci uz sat: dvoklik otvara goCOP, desni klik nudi pokretanje, zaustavljanje i nadogradnju.
ClickFinish=Kliknite Završi.
ConfirmUninstall=Ukloniti goCOP s ovog računala? Podaci (baza, postavke) ostaju dok ih izričito ne obrišete.
UninstallStatusLabel=Uklanjam goCOP…
UninstalledAll=goCOP je uklonjen.
UninstalledMost=goCOP je uklonjen. Neke datoteke nisu se mogle obrisati; možete ih obrisati ručno.

[Tasks]
Name: "prijava"; Description: "Pokreni goCOP pri prijavi u Windows"

[Files]
Source: "..\dist\gocop-postava.exe"; DestDir: "{app}\postava"; Flags: ignoreversion

[Icons]
Name: "{autoprograms}\goCOP\goCOP"; Filename: "{app}\postava\gocop-postava.exe"; Comment: "goCOP: ikona u traci, pokretanje i nadogradnja čvora"
Name: "{autoprograms}\goCOP\Ukloni goCOP"; Filename: "{uninstallexe}"; Comment: "Ukloni goCOP s ovog računala"

[InstallDelete]
; prečac Postave 1.0.0 (bez mape)
Type: files; Name: "{autoprograms}\goCOP.lnk"

[Run]
Filename: "{app}\postava\gocop-postava.exe"; Parameters: "-instaliraj -pri-prijavi={code:PriPrijavi}{code:IzMape}{code:ImeCvora}{code:Prvi}"; StatusMsg: "Preuzimam najnovije izdanje goCOP-a i provjeravam potpis…"; Flags: waituntilterminated
Filename: "{app}\postava\gocop-postava.exe"; Description: "Pokreni goCOP"; Flags: postinstall nowait skipifsilent

[UninstallRun]
Filename: "{app}\postava\gocop-postava.exe"; Parameters: "-ukloni"; RunOnceId: "ukloni"; Flags: waituntilterminated runhidden

[UninstallDelete]
Type: filesandordirs; Name: "{app}\program"
Type: files; Name: "{app}\postava.pid"
Type: files; Name: "{app}\postava.lock"

[Code]
const
  KljucDeinstalacije = 'Software\Microsoft\Windows\CurrentVersion\Uninstall\{6F1D2C7A-3B4E-4C1F-9A2E-60C0B0FA0C01}_is1';

var
  StranicaImena: TInputQueryWizardPage;
  StranicaMreze: TInputOptionWizardPage;

{ ime čvora: mala slova, brojke i crtica (internal/imecvora) }
function OcistiIme(S: String): String;
var
  I: Integer;
  C: String;
  R: String;
  Crtica: Boolean;
begin
  R := '';
  Crtica := False;
  for I := 1 to Length(S) do
  begin
    C := Lowercase(Copy(S, I, 1));
    if (C = 'č') or (C = 'ć') or (C = 'Č') or (C = 'Ć') then C := 'c';
    if (C = 'đ') or (C = 'Đ') then C := 'd';
    if (C = 'š') or (C = 'Š') then C := 's';
    if (C = 'ž') or (C = 'Ž') then C := 'z';
    if ((C >= 'a') and (C <= 'z') and (Length(C) = 1)) or ((C >= '0') and (C <= '9') and (Length(C) = 1)) then
    begin
      R := R + C;
      Crtica := False;
    end
    else if (not Crtica) and (R <> '') then
    begin
      R := R + '-';
      Crtica := True;
    end;
  end;
  if Length(R) > 40 then R := Copy(R, 1, 40);
  while (Length(R) > 0) and (Copy(R, Length(R), 1) = '-') do
    R := Copy(R, 1, Length(R) - 1);
  Result := R;
end;

function ValjanoIme(S: String): Boolean;
begin
  Result := (Length(S) >= 3) and (Length(S) <= 40) and (OcistiIme(S) = S) and (Pos('--', S) = 0);
end;

{ goCOP je već instaliran: ponovno pokretanje nudi popravak ili uklanjanje }
function InitializeSetup(): Boolean;
var
  Deinstalacija: String;
  Odabir, Kod: Integer;
begin
  Result := True;
  if WizardSilent then Exit;
  if not RegQueryStringValue(HKCU, KljucDeinstalacije, 'UninstallString', Deinstalacija) then Exit;
  Odabir := TaskDialogMsgBox('goCOP je već instaliran na ovom računalu.',
    'Popravak ili nadogradnja ponovno postavlja Postavu i preuzima najnovije izdanje goCOP-a; podaci i ime čvora ostaju. Uklanjanje briše program, a podatke samo ako to na kraju izričito potvrdite.',
    mbConfirmation, MB_YESNOCANCEL, ['Popravi ili nadogradi', 'Ukloni goCOP'], 0);
  if Odabir = IDYES then Exit;
  Result := False;
  if Odabir = IDNO then
    Exec(RemoveQuotes(Deinstalacija), '', '', SW_SHOW, ewNoWait, Kod);
end;

procedure InitializeWizard();
begin
  StranicaImena := CreateInputQueryPage(wpSelectDir,
    'Ime ovog računala u mreži',
    'Kako se ovo računalo zove u mreži goCOP-a?',
    'Ime vide svi čvorovi mreže, a pod njim računalo upisuje svoje zapise. VAŽNO: nakon instalacije ime se više ne može promijeniti, i ne smije ga imati nijedno drugo računalo u mreži.' + #13#10#13#10 +
    'Mala slova, brojke i crtica, npr. pperic-thinkpad ili pperic-laptop-sluzbeni.');
  StranicaImena.Add('Ime:', False);
  StranicaImena.Values[0] := OcistiIme(GetUserNameString + '-' + GetComputerNameString);

  StranicaMreze := CreateInputOptionPage(StranicaImena.ID,
    'Važno: nova ili postojeća mreža',
    'Je li ovo prvo računalo nove mreže? Pažljivo izaberite.',
    'Nova mreža je za prvo računalo, jednom po mreži (npr. prvi čvor centra): na njemu napravite svoj administratorski račun, upišete ustroj i djelatnike, a ono prima ostala računala.' + #13#10#13#10 +
    'Postojeća mreža je za svako sljedeće računalo (laptop djelatnika, uredski PC): uparite ga s računalom koje je već u mreži, pa djelatnici, registri i podaci stižu od tamo.' + #13#10#13#10 +
    'Ako niste sigurni, izaberite postojeću: nova mreža je zaseban svijet i kasnije se ne može spojiti s drugima.',
    True, False);
  StranicaMreze.Add('Priključit ću ga postojećoj mreži (sljedeće računalo)');
  StranicaMreze.Add('Ovo je prvo računalo nove mreže');
  StranicaMreze.SelectedValueIndex := 0;
end;

{ čvor već postoji (ponovna instalacija preko podataka): ime i mreža ostaju }
function PostojeciCvor(): Boolean;
begin
  Result := FileExists(ExpandConstant('{app}\data\gocop.toml')) or FileExists(ExpandConstant('{app}\data\gocop.db'));
end;

function ShouldSkipPage(PageID: Integer): Boolean;
begin
  Result := ((PageID = StranicaImena.ID) or (PageID = StranicaMreze.ID)) and PostojeciCvor();
end;

function NextButtonClick(CurPageID: Integer): Boolean;
begin
  Result := True;
  if (CurPageID = StranicaMreze.ID) and (StranicaMreze.SelectedValueIndex = 1) then
    Result := MsgBox('Ovo računalo bit će PRVO računalo NOVE mreže.' + #13#10#13#10 +
      'To se radi samo jednom po mreži, npr. za prvi čvor centra. Ono drži ključ mreže i prima sva ostala računala.' + #13#10#13#10 +
      'Nova mreža je zaseban svijet: ne vidi podatke drugih mreža i kasnije se ne može spojiti s njima. Ako ovo računalo treba raditi s postojećim računalima (ured, Unraid, drugi laptopi), izaberite postojeću mrežu.' + #13#10#13#10 +
      'Je li ovo doista prvo računalo nove mreže?', mbConfirmation, MB_YESNO or MB_DEFBUTTON2) = IDYES;
  if CurPageID = StranicaImena.ID then
  begin
    StranicaImena.Values[0] := Trim(StranicaImena.Values[0]);
    if not ValjanoIme(StranicaImena.Values[0]) then
    begin
      MsgBox('Ime ima 3 do 40 znakova: mala slova bez kvačica, brojke i crticu (ne na početku ni kraju). Prijedlog: ' + OcistiIme(StranicaImena.Values[0]), mbError, MB_OK);
      Result := False;
    end;
  end;
end;

function ImeCvora(Param: String): String;
begin
  Result := '';
  if not PostojeciCvor() then
    Result := ' -ime-cvora=' + StranicaImena.Values[0];
end;

function Prvi(Param: String): String;
begin
  Result := '';
  if PostojeciCvor() then Exit;
  if StranicaMreze.SelectedValueIndex = 1 then
    Result := ' -prvi=nova'
  else
    Result := ' -prvi=postojeca';
end;

function PriPrijavi(Param: String): String;
begin
  if WizardIsTaskSelected('prijava') then
    Result := 'true'
  else
    Result := 'false';
end;

{ izdanje bez interneta: program, SHA256SUMS i potpis uz instalacijski program }
function IzMape(Param: String): String;
begin
  Result := '';
  if FileExists(ExpandConstant('{src}\gocop-windows-amd64.exe')) and
     FileExists(ExpandConstant('{src}\SHA256SUMS')) and
     FileExists(ExpandConstant('{src}\SHA256SUMS.sig')) then
    Result := ' -iz-mape "' + ExpandConstant('{src}') + '"';
end;

{ postojeća Postava se gasi prije zamjene datoteka (ponovna instalacija) }
function PrepareToInstall(var NeedsRestart: Boolean): String;
var
  Postava: String;
  Kod: Integer;
begin
  Result := '';
  Postava := ExpandConstant('{app}\postava\gocop-postava.exe');
  if FileExists(Postava) then
    Exec(Postava, '-zaustavi', '', SW_HIDE, ewWaitUntilTerminated, Kod);
end;

{ podaci ostaju, osim ako korisnik izričito traži brisanje }
procedure CurUninstallStepChanged(CurUninstallStep: TUninstallStep);
var
  Podaci: String;
begin
  if CurUninstallStep = usPostUninstall then
  begin
    Podaci := ExpandConstant('{app}\data');
    if DirExists(Podaci) and (UninstallSilent = False) then
      if MsgBox('Obrisati i podatke goCOP-a u ' + Podaci + '?' + #13#10#13#10 +
                'To su baza, ključ čvora, postavke i kopije baze. Brisanje se ne može vratiti.',
                mbConfirmation, MB_YESNO or MB_DEFBUTTON2) = IDYES then
        DelTree(Podaci, True, True, True);
  end;
end;
