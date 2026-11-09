Unicode true

####
## Moxie combined Windows installer (desktop + CLI).
##
## This is the Wails NSIS template, customised to install TWO binaries into one
## per-user directory:
##   * the Wails desktop app  -> ${MOXIE_DESKTOP_EXE}
##   * the moxie CLI          -> ${MOXIE_CLI_EXE}  (added to the user PATH)
##
## Per-build values (version, channel, names, staged CLI paths) live in the
## generated file "moxie_build.nsh", written by scripts/package-windows.ps1
## immediately before `wails build --nsis`. NSIS runs makensis with its cwd set
## to this directory, so the staged CLI files are referenced by bare filename.
##
## To build manually for debugging: generate moxie_build.nsh first (or accept
## the fallback defaults below), then:
##   wails build --platform windows/amd64,windows/arm64 --nsis --installscope user
####

!if /FileExists "moxie_build.nsh"
    !include "moxie_build.nsh"
!else
    ## Fallback so this template still parses without the generated file.
    !define MOXIE_CHANNEL "main"
    !define MOXIE_VERSION "0.0.0"
    !define MOXIE_PRODUCT_NAME "Moxie"
    !define MOXIE_SETUP_NAME "Moxie-Setup"
    !define MOXIE_CLI_EXE "moxie.exe"
    !define MOXIE_DESKTOP_EXE "moxie-desktop.exe"
    !define MOXIE_CLI_AMD64 "moxie-cli-amd64.exe"
    !define MOXIE_CLI_ARM64 "moxie-cli-arm64.exe"
!endif

## ProjectInfo values. These MUST be defined before "wails_tools.nsh" is
## included: that file only supplies defaults for names it does not already see.
!define INFO_PROJECTNAME "moxie"
!define INFO_COMPANYNAME "Milisource"
!define INFO_PRODUCTNAME "${MOXIE_PRODUCT_NAME}"
!define INFO_PRODUCTVERSION "${MOXIE_VERSION}"
!define INFO_COPYRIGHT "Copyright (c) 2026 Milisource"

## The desktop app installs under PRODUCT_EXECUTABLE. Overriding it here (before
## wails_tools.nsh) keeps it distinct from the CLI's moxie.exe.
!define PRODUCT_EXECUTABLE "${MOXIE_DESKTOP_EXE}"
!define UNINST_KEY_NAME "Moxie-${MOXIE_CHANNEL}-${MOXIE_PRODUCT_NAME}"

## Marker key: records that this installer appended $INSTDIR to the user PATH,
## so uninstall removes exactly what we added and reinstall stays idempotent.
!define MOXIE_STATE_KEY "Software\Moxie\Installer\${MOXIE_CHANNEL}"

!include "wails_tools.nsh"
!include "WinMessages.nsh"
!include "LogicLib.nsh"
!include "WordFunc.nsh"

VIProductVersion "${INFO_PRODUCTVERSION}.0"
VIFileVersion    "${INFO_PRODUCTVERSION}.0"

VIAddVersionKey "CompanyName"     "${INFO_COMPANYNAME}"
VIAddVersionKey "FileDescription" "${INFO_PRODUCTNAME} Installer"
VIAddVersionKey "ProductVersion"  "${INFO_PRODUCTVERSION}"
VIAddVersionKey "FileVersion"     "${INFO_PRODUCTVERSION}"
VIAddVersionKey "LegalCopyright"  "${INFO_COPYRIGHT}"
VIAddVersionKey "ProductName"     "${INFO_PRODUCTNAME}"

ManifestDPIAware true

!include "MUI.nsh"

!define MUI_ICON "..\icon.ico"
!define MUI_UNICON "..\icon.ico"
!define MUI_FINISHPAGE_NOAUTOCLOSE
!define MUI_ABORTWARNING
!define MUI_FINISHPAGE_RUN "$INSTDIR\${PRODUCT_EXECUTABLE}"
!define MUI_FINISHPAGE_RUN_TEXT "Launch ${INFO_PRODUCTNAME}"

!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH

!insertmacro MUI_UNPAGE_INSTFILES

!insertmacro MUI_LANGUAGE "English"

Name "${INFO_PRODUCTNAME}"
OutFile "..\..\bin\${MOXIE_SETUP_NAME}.exe"
InstallDir "$LOCALAPPDATA\Programs\${INFO_PRODUCTNAME}"
ShowInstDetails show

## ── PATH helpers ───────────────────────────────────────────────────────────
## Append $INSTDIR to the per-user PATH (HKCU\Environment) once, and broadcast
## WM_SETTINGCHANGE so new shells pick it up without a logout.
!macro moxie.addToPath
    ReadRegStr $1 HKCU "${MOXIE_STATE_KEY}" "PathAdded"
    ${If} $1 != "1"
        ReadRegStr $0 HKCU "Environment" "Path"
        ${If} $0 == ""
            StrCpy $2 "$INSTDIR"
        ${Else}
            StrCpy $2 "$0;$INSTDIR"
        ${EndIf}
        WriteRegStr HKCU "Environment" "Path" "$2"
        WriteRegStr HKCU "${MOXIE_STATE_KEY}" "PathAdded" "1"
        SendMessage ${HWND_BROADCAST} ${WM_SETTINGCHANGE} 0 "STR:Environment" /TIMEOUT=5000
    ${EndIf}
!macroend

!macro moxie.removeFromPath
    ReadRegStr $1 HKCU "${MOXIE_STATE_KEY}" "PathAdded"
    ${If} $1 == "1"
        ReadRegStr $0 HKCU "Environment" "Path"
        ${WordReplace} "$0" "$INSTDIR;" "" "+" $2
        ${WordReplace} "$2" ";$INSTDIR" "" "+" $3
        ${WordReplace} "$3" "$INSTDIR" "" "+" $4
        WriteRegStr HKCU "Environment" "Path" "$4"
        SendMessage ${HWND_BROADCAST} ${WM_SETTINGCHANGE} 0 "STR:Environment" /TIMEOUT=5000
    ${EndIf}
    DeleteRegKey HKCU "${MOXIE_STATE_KEY}"
!macroend

Function .onInit
    !insertmacro wails.checkArchitecture
FunctionEnd

Section
    !insertmacro wails.setShellContext
    !insertmacro wails.webview2runtime

    SetOutPath $INSTDIR

    ## Desktop app (per native architecture, as ${PRODUCT_EXECUTABLE}).
    !insertmacro wails.files

    ## CLI, matching the native architecture, installed as ${MOXIE_CLI_EXE}.
    !ifdef SUPPORTS_AMD64
        ${If} ${IsNativeAMD64}
            File "/oname=${MOXIE_CLI_EXE}" "${MOXIE_CLI_AMD64}"
        ${EndIf}
    !endif
    !ifdef SUPPORTS_ARM64
        ${If} ${IsNativeARM64}
            File "/oname=${MOXIE_CLI_EXE}" "${MOXIE_CLI_ARM64}"
        ${EndIf}
    !endif

    !insertmacro moxie.addToPath

    CreateShortcut "$SMPROGRAMS\${INFO_PRODUCTNAME}.lnk" "$INSTDIR\${PRODUCT_EXECUTABLE}"
    CreateShortCut "$DESKTOP\${INFO_PRODUCTNAME}.lnk" "$INSTDIR\${PRODUCT_EXECUTABLE}"

    !insertmacro wails.associateFiles
    !insertmacro wails.associateCustomProtocols

    !insertmacro wails.writeUninstaller
SectionEnd

Section "uninstall"
    !insertmacro wails.setShellContext

    RMDir /r "$AppData\${PRODUCT_EXECUTABLE}"

    !insertmacro moxie.removeFromPath

    Delete "$SMPROGRAMS\${INFO_PRODUCTNAME}.lnk"
    Delete "$DESKTOP\${INFO_PRODUCTNAME}.lnk"

    RMDir /r $INSTDIR

    !insertmacro wails.unassociateFiles
    !insertmacro wails.unassociateCustomProtocols

    !insertmacro wails.deleteUninstaller
SectionEnd
