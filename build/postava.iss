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
WelcomeLabel2=Instalira se goCOP Postava {#Verzija} za ovog korisnika Windowsa, bez administratorskih prava.%n%nPostava će zatim s GitHuba preuzeti najnovije izdanje goCOP-a, provjeriti mu potpis i pokrenuti čvor. Ikona će biti u traci uz sat.
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
Name: "{autoprograms}\goCOP"; Filename: "{app}\postava\gocop-postava.exe"; Comment: "goCOP: ikona u traci, pokretanje i nadogradnja čvora"

[Run]
Filename: "{app}\postava\gocop-postava.exe"; Parameters: "-instaliraj -pri-prijavi={code:PriPrijavi}{code:IzMape}"; StatusMsg: "Preuzimam najnovije izdanje goCOP-a i provjeravam potpis…"; Flags: waituntilterminated
Filename: "{app}\postava\gocop-postava.exe"; Description: "Pokreni goCOP"; Flags: postinstall nowait skipifsilent

[UninstallRun]
Filename: "{app}\postava\gocop-postava.exe"; Parameters: "-ukloni"; RunOnceId: "ukloni"; Flags: waituntilterminated runhidden

[UninstallDelete]
Type: filesandordirs; Name: "{app}\program"
Type: files; Name: "{app}\postava.pid"
Type: files; Name: "{app}\postava.lock"

[Code]
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
