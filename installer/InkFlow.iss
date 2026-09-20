; Windows x64 per-user installer. Application files and user data are deliberately separated:
;   {localappdata}\Programs\InkFlow  - replaceable program files
;   {localappdata}\InkFlow           - SQLite, USearch indexes, models, logs and backups
; The uninstaller must never delete the latter directory.

#define AppName "InkFlow"
#define AppVersion GetEnv("INKFLOW_VERSION")
#if AppVersion == ""
  #define AppVersion "0.1.0"
#endif
#define AppPublisher "InkFlow"
#define AppExeName "InkFlow.exe"
#define BuildDir "..\build\package"
#define OutputDir "..\build\installer"
#define PackageName GetEnv("INKFLOW_PACKAGE_NAME")
#if PackageName == ""
  #define PackageName "InkFlow-Setup-{#AppVersion}-x64"
#endif

; These model identifiers must match config/local_models.go. Models are stored in
; the user-data directory instead of {app}, so an application upgrade never
; overwrites an already downloaded model.
#define HuggingFaceOfficialURL "https://huggingface.co"
#define HuggingFaceChinaMirrorURL "https://hf-mirror.com"
#define EmbeddingModelRepo "enacimie/Qwen3-Embedding-0.6B-Q4_K_M-GGUF"
#define EmbeddingModelFile "qwen3-embedding-0.6b-q4_k_m.gguf"
#define RerankModelRepo "gpustack/bge-reranker-v2-m3-GGUF"
#define RerankModelFile "bge-reranker-v2-m3-Q4_K_M.gguf"

[Setup]
AppId={{9AF1CEAF-4C7C-483B-9B7C-E7B7A3E11E5D}
AppName={#AppName}
AppVersion={#AppVersion}
AppPublisher={#AppPublisher}
DefaultDirName={localappdata}\Programs\{#AppName}
DefaultGroupName={#AppName}
DisableProgramGroupPage=yes
PrivilegesRequired=lowest
PrivilegesRequiredOverridesAllowed=dialog
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
OutputDir={#OutputDir}
OutputBaseFilename={#PackageName}
Compression=lzma2/ultra64
SolidCompression=yes
WizardStyle=modern
UninstallDisplayName={#AppName}
VersionInfoVersion={#AppVersion}
SetupLogging=yes
SetupIconFile=..\assets\inkflow.ico

[Languages]
Name: "english"; MessagesFile: "compiler:Default.isl"

[Tasks]
Name: "desktopicon"; Description: "创建桌面快捷方式"; GroupDescription: "附加任务："; Flags: unchecked

[Files]
Source: "{#BuildDir}\*"; DestDir: "{app}"; Flags: ignoreversion recursesubdirs createallsubdirs; Excludes: "config.yaml"
; Seed the editable desktop configuration once. Do not overwrite it during an
; upgrade: users may tune GPU layers, batch settings, and model paths here.
Source: "{#BuildDir}\config.yaml"; DestDir: "{localappdata}\InkFlow"; DestName: "config.yaml"; Flags: ignoreversion onlyifdoesntexist
; pp_doclayout_s.onnx 与 onnxruntime.dll 都是随应用升级的只读发布资源，放在 {app}
; 中由安装包统一替换；用户数据目录不保存 OCR 模型副本。

[Icons]
Name: "{autoprograms}\{#AppName}"; Filename: "{app}\{#AppExeName}"; WorkingDir: "{app}"
Name: "{autodesktop}\{#AppName}"; Filename: "{app}\{#AppExeName}"; WorkingDir: "{app}"; Tasks: desktopicon

[Run]
Filename: "{app}\{#AppExeName}"; Description: "启动 {#AppName}"; Flags: nowait postinstall skipifsilent

; Do not add [UninstallDelete] entries for {localappdata}\InkFlow.
; That directory contains the user's SQLite database, USearch HNSW indexes, models and backups.

[Code]
var
  ModelSourcePage: TInputOptionWizardPage;
  ModelEndpointPage: TInputQueryWizardPage;

function TrimTrailingSlashes(const Value: String): String;
begin
  Result := Trim(Value);
  while (Length(Result) > 0) and (Result[Length(Result)] = '/') do
    Delete(Result, Length(Result), 1);
end;

function IsModelDownloadRequested(): Boolean;
begin
  Result := (not WizardSilent) and (ModelSourcePage.SelectedValueIndex > 0);
end;

function ModelDownloadBaseURL(): String;
begin
  if ModelSourcePage.SelectedValueIndex = 3 then
    Result := TrimTrailingSlashes(ModelEndpointPage.Values[0])
  else if ModelSourcePage.SelectedValueIndex = 2 then
    Result := '{#HuggingFaceChinaMirrorURL}'
  else
    Result := '{#HuggingFaceOfficialURL}';
end;

function ModelDownloadURL(const Repository, FileName: String): String;
begin
  Result := ModelDownloadBaseURL() + '/' + Repository + '/resolve/main/' + FileName + '?download=true';
end;

function OnModelDownloadProgress(const Url, FileName: String; const Progress, ProgressMax: Int64): Boolean;
var
  Percent: Integer;
begin
  if ProgressMax > 0 then
  begin
    Percent := (Progress * 100) div ProgressMax;
    WizardForm.StatusLabel.Caption := Format('Downloading local model %s: %d%%', [FileName, Percent]);
  end
  else
    WizardForm.StatusLabel.Caption := Format('Downloading local model %s', [FileName]);
  Result := True;
end;

function DownloadModel(const Repository, FileName: String): Boolean;
var
  TargetPath: String;
  TemporaryPath: String;
begin
  TargetPath := ExpandConstant('{localappdata}\InkFlow\models\') + FileName;
  if FileExists(TargetPath) then
  begin
    Log('Local model already exists, skip download: ' + TargetPath);
    Result := True;
    Exit;
  end;

  if not ForceDirectories(ExtractFileDir(TargetPath)) then
  begin
    Log('Cannot create local model directory: ' + ExtractFileDir(TargetPath));
    Result := False;
    Exit;
  end;

  try
    DownloadTemporaryFile(ModelDownloadURL(Repository, FileName), FileName, '', @OnModelDownloadProgress);
    TemporaryPath := ExpandConstant('{tmp}\') + FileName;
    if not CopyFile(TemporaryPath, TargetPath, False) then
      RaiseException('Cannot move downloaded model to ' + TargetPath);
    Log('Downloaded local model: ' + TargetPath);
    Result := True;
  except
    Log('Failed to download local model ' + FileName + ': ' + GetExceptionMessage);
    Result := False;
  end;
end;

procedure DownloadSelectedModels();
var
  EmbeddingOK: Boolean;
  RerankOK: Boolean;
begin
  WizardForm.StatusLabel.Caption := 'Preparing local RAG model download...';
  EmbeddingOK := DownloadModel('{#EmbeddingModelRepo}', '{#EmbeddingModelFile}');
  RerankOK := DownloadModel('{#RerankModelRepo}', '{#RerankModelFile}');
  if EmbeddingOK and RerankOK then
    WizardForm.StatusLabel.Caption := 'Local RAG models are ready.'
  else
    MsgBox(
      'InkFlow was installed, but one or more local RAG models could not be downloaded. ' +
      'You can download the missing GGUF files later into {localappdata}\InkFlow\models.',
      mbInformation,
      MB_OK
    );
end;

procedure InitializeWizard();
begin
  ModelSourcePage := CreateInputOptionPage(
    wpSelectTasks,
    'Local RAG models',
    'Choose whether to download the local embedding and rerank models now',
    'The models are large and are stored in your local InkFlow data directory. Existing model files are kept during upgrades.',
    True,
    False
  );
  ModelSourcePage.Add('Do not download now (download later)');
  ModelSourcePage.Add('Hugging Face official');
  ModelSourcePage.Add('hf-mirror.com (China mirror)');
  ModelSourcePage.Add('Custom Hugging Face-compatible endpoint');
  ModelSourcePage.SelectedValueIndex := 0;

  ModelEndpointPage := CreateInputQueryPage(
    ModelSourcePage.ID,
    'Custom model download endpoint',
    'Use a Hugging Face-compatible endpoint',
    'Enter the HTTPS base URL. It must expose the same repository and file paths as Hugging Face.'
  );
  ModelEndpointPage.Add('HTTPS base URL:', False);
  ModelEndpointPage.Values[0] := '{#HuggingFaceOfficialURL}';
end;

function ShouldSkipPage(PageID: Integer): Boolean;
begin
  Result := (PageID = ModelEndpointPage.ID) and (ModelSourcePage.SelectedValueIndex <> 3);
end;

function NextButtonClick(CurPageID: Integer): Boolean;
var
  Endpoint: String;
begin
  Result := True;
  if CurPageID = ModelEndpointPage.ID then
  begin
    Endpoint := Lowercase(TrimTrailingSlashes(ModelEndpointPage.Values[0]));
    if Pos('https://', Endpoint) <> 1 then
    begin
      MsgBox('Please enter an HTTPS base URL.', mbError, MB_OK);
      Result := False;
    end;
  end;
end;

procedure CurStepChanged(CurStep: TSetupStep);
begin
  // Download before [Run] executes the application, so the first launch can
  // initialize both backend models immediately.
  if (CurStep = ssInstall) and IsModelDownloadRequested() then
    DownloadSelectedModels();
end;
