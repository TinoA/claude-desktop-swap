# Plan de corrección y aceleración del swap

## Propósito

Este documento resume una revisión estática del cambio de cuenta en Windows y sirve como referencia para validar la implementación. Su objetivo es que otro agente pueda cuestionar las decisiones propuestas y sugerir una solución más segura o simple.

## Estado de implementación

La implementación actual ya incorpora estas partes del plan:

- Detección de MSIX, Squirrel/Win32 y portable, incluyendo procesos Squirrel `app-*`.
- Exclusión de Claude Code por ruta y bloqueo si hay dos instalaciones distintas abiertas.
- Checkpoint aunque Claude ya estuviera cerrado.
- Fast path cuando la sesión viva ya corresponde al perfil elegido.
- Rollback conjunto de Cookies, Local Storage y almacenes efímeros.
- Recuperación de la sesión anterior ante errores previos al commit.
- Backup local protegido por el usuario de Windows y backup portable con contraseña.
- Importación automática según el tipo de backup.
- Overlays, formularios, prompts y selectores de archivo nativos de Windows.
- Caché de detección y eliminación de sincronización duplicada de snapshots en Windows.

Validado hasta ahora con `go test ./...`, `go vet ./...`, una prueba real de detección contra la instalación Squirrel local y un ciclo controlado de abrir/cerrar el overlay nativo. El swap de cuentas real debe hacerse con el ejecutable final compilado por el flujo de distribución, manteniendo Claude instalado.

Archivos principales revisados:

- `cmd/use.go`
- `cmd/use_test.go`
- `cmd/tray_windows.go`
- `cmd/switch_overlay_windows.go`
- `internal/platform/windows.go`
- `internal/profile/store.go`

## Resumen ejecutivo

El retraso no parece deberse al tamaño de los perfiles. Los dos perfiles locales revisados ocupan aproximadamente 3.9–4.2 MB y tienen diez archivos LevelDB cada uno. Los mayores costos probables son:

1. Detección repetida de Claude mediante PowerShell.
2. Creación de un proceso PowerShell/WPF para cada overlay.
3. Sincronizaciones a disco duplicadas.
4. Validaciones repetidas del mismo perfil.
5. Mantener el overlay abierto mientras se comprueba el arranque de Electron.

Antes de optimizar hay que corregir cuatro riesgos de integridad:

1. El archivo `current` se trata como verdad aunque la sesión viva sea distinta.
2. Se omite el checkpoint saliente cuando Claude ya estaba cerrado.
3. El rollback de restore solo recupera Cookies, no Local Storage.
4. Un error después de cerrar Claude puede dejarlo cerrado sin recuperar la cuenta anterior.

La reinstalación también confirmó que la detección actual no reconoce el instalador individual Squirrel y elegiría el portable `ClaudeChatOnly` aunque la instalación oficial registrada esté abierta. Esta corrección pasa a ser requisito previo a cualquier prueba real de swap.

## Flujo actual

```text
clic en cuenta
  -> adquirir operation.lock
  -> iniciar PowerShell/WPF del overlay
  -> detectar instalación con otro PowerShell/Get-AppxPackage
  -> validar perfil destino
  -> detectar proceso
  -> leer marcador current
  -> cerrar Claude
  -> si current == destino: iniciar Claude sin verificar sesión viva
  -> si Claude estaba abierto: checkpoint de la cuenta saliente
  -> restore del destino
  -> iniciar Claude y esperar 5 sondeos de proceso
  -> cerrar overlay
```

## Evidencia de rendimiento

### Detección de AppX

La consulta usada por `appxInstallLocation()` se midió tres veces en esta máquina:

```text
474.37 ms
450.99 ms
398.86 ms
promedio: 441.4 ms
```

Cada llamada a `platform.Current()` vuelve a ejecutar esa detección. El overlay también ejecuta su propio `Get-AppxPackage` para obtener el icono.

### Tamaño de perfiles

```text
hgj          12 archivos   4,222,578 bytes   LevelDB: 4,193,502 bytes
install-test 12 archivos   3,873,704 bytes   LevelDB: 3,840,523 bytes
```

El volumen es pequeño. Si el swap tarda varios segundos, el costo está en creación de procesos, `FlushFileBuffers`, espera de Electron o antivirus, no en transferir muchos datos.

## Revisión de detección después de reinstalar Claude

### Resultado observado en esta máquina

La reinstalación del 12 de julio de 2026 no quedó registrada como AppX/MSIX. El instalador individual creó una instalación Squirrel por usuario:

```text
Tipo observado:       Squirrel/Win32 por usuario
Registro:              HKCU\Software\Microsoft\Windows\CurrentVersion\Uninstall\AnthropicClaude
InstallLocation:       %LOCALAPPDATA%\AnthropicClaude
Launcher estable:      %LOCALAPPDATA%\AnthropicClaude\claude.exe
Ejecutable en uso:     %LOCALAPPDATA%\AnthropicClaude\app-1.20186.1\claude.exe
Acceso de Inicio:      %APPDATA%\Microsoft\Windows\Start Menu\Programs\Anthropic\Claude.lnk
Datos Electron:        %APPDATA%\Claude
Cookies:               %APPDATA%\Claude\Network\Cookies
Versión observada:     1.20186.1
Firma Authenticode:    válida, Anthropic, PBC
```

`Get-AppxPackage -Name Claude` no devuelve ningún paquete en esta instalación. Anthropic documenta actualmente dos vías de Windows que deben contemplarse: instalador individual y MSIX x64/arm64 para despliegue administrado. Fuentes oficiales:

- <https://support.claude.com/en/articles/10065433-install-claude-desktop>
- <https://support.claude.com/en/articles/12622703-deploy-claude-desktop-for-windows>

### Comportamiento del detector actual

El detector actual no encontrará correctamente esta reinstalación:

1. Busca MSIX mediante `Get-AppxPackage`; ahora no existe.
2. Busca primero el portable administrado en `%USERPROFILE%\ClaudeChatOnly\app\claude.exe`; todavía existe y por tanto lo selecciona.
3. Nunca llega a la instalación registrada en `%LOCALAPPDATA%\AnthropicClaude` porque esa ruta no está entre sus candidatos.
4. Si se elimina el portable, tampoco encuentra Claude: revisa `%LOCALAPPDATA%\Claude` y `%LOCALAPPDATA%\Programs\Claude`, pero no `%LOCALAPPDATA%\AnthropicClaude`.

Esto no es solamente un problema al iniciar. `desktopProcesses()` exige que la ruta del proceso coincida exactamente con `w.executable`. En Squirrel:

```text
ruta usada para iniciar:  ...\AnthropicClaude\claude.exe
ruta real del proceso:    ...\AnthropicClaude\app-1.20186.1\claude.exe
```

Por eso **no basta con agregar `%LOCALAPPDATA%\AnthropicClaude\claude.exe` a la lista actual**. El switcher seguiría sin reconocer los procesos versionados.

Con el código actual puede ocurrir esta secuencia peligrosa:

```text
Claude oficial Squirrel está abierto
  -> el switcher selecciona ClaudeChatOnly
  -> IsRunning devuelve false porque compara otra ruta
  -> KillApp no cierra el Claude oficial
  -> RestoreAt intenta modificar %APPDATA%\Claude mientras Electron lo usa
  -> se puede obtener bloqueo SQLite, restore parcial o datos reescritos por Claude
  -> LaunchApp abre además el portable
  -> quedan varias instancias de Claude
```

En esta máquina también existe un proceso `claude.exe` de Claude Code bajo `%APPDATA%\Claude\claude-code\...`. Cualquier mejora que haga matching por nombre solamente podría cerrar Claude Code por error. El matching debe ser por instalación propietaria y ruta canónica.

### D1. El modelo actual de instalación es insuficiente

`windowsPlatform` guarda solamente:

```go
root       string
executable string
msix       bool
```

Una instalación Squirrel necesita separar al menos:

- Ruta de datos Electron.
- Launcher estable.
- Raíz permitida para los procesos versionados.
- Tipo de instalación.
- Identidad persistente de la instalación.
- AUMID únicamente para MSIX.

Modelo sugerido para `internal/platform/windows.go`:

```go
type windowsInstallKind uint8

const (
    installUnknown windowsInstallKind = iota
    installSquirrel
    installMSIX
    installPortable
    installWin32
)

type windowsInstall struct {
    id             string
    kind           windowsInstallKind
    dataRoot       string
    launchTarget   string
    processRoot    string
    packageFamily  string
    aumid          string
    version         string
}

type windowsPlatform struct {
    install windowsInstall
}
```

Para Squirrel en esta máquina:

```text
dataRoot     = %APPDATA%\Claude
launchTarget = %LOCALAPPDATA%\AnthropicClaude\claude.exe
processRoot  = %LOCALAPPDATA%\AnthropicClaude
kind         = installSquirrel
```

La ruta `app-<versión>` no debe persistirse como launcher porque cambia con cada actualización.

### D2. Descubrimiento recomendado

El detector debe construir una lista de candidatos y elegir después, en vez de retornar al encontrar el primer archivo. Orden recomendado de fuentes:

1. Instalación actualmente abierta y reconocida.
2. Instalación elegida anteriormente y todavía válida.
3. Squirrel registrada en HKCU/HKLM.
4. MSIX registrado para el usuario actual.
5. `App Paths` y claves de uninstall de instaladores Win32/MSI.
6. Acceso directo de Inicio firmado por Anthropic.
7. Rutas canónicas conocidas.
8. Portable administrado por el switcher.
9. Override explícito del usuario, si se decide soportarlo.

El override podría tener máxima prioridad cuando exista, pero debe ser una decisión explícita. Una variable posible sería `CLAUDE_DESKTOP_EXE`.

No se recomienda escanear recursivamente el disco. Es lento, puede producir falsos positivos y encontrar Claude Code antes que Claude Desktop.

#### Detector Squirrel por registro

Ubicación propuesta: `internal/platform/windows.go`. La dependencia `golang.org/x/sys` ya está presente.

```go
import "golang.org/x/sys/windows/registry"

const squirrelUninstallKey = `Software\Microsoft\Windows\CurrentVersion\Uninstall\AnthropicClaude`

func detectSquirrelInstall() (windowsInstall, bool) {
    key, err := registry.OpenKey(
        registry.CURRENT_USER,
        squirrelUninstallKey,
        registry.QUERY_VALUE,
    )
    if err != nil {
        return windowsInstall{}, false
    }
    defer key.Close()

    location, _, err := key.GetStringValue("InstallLocation")
    if err != nil || location == "" {
        return windowsInstall{}, false
    }
    version, _, _ := key.GetStringValue("DisplayVersion")
    launcher := filepath.Join(location, "claude.exe")
    if !regularFile(launcher) {
        return windowsInstall{}, false
    }

    return windowsInstall{
        id:           "squirrel:" + normalizeWindowsPath(location),
        kind:         installSquirrel,
        dataRoot:     filepath.Join(os.Getenv("APPDATA"), "Claude"),
        launchTarget: launcher,
        processRoot:  location,
        version:      version,
    }, true
}
```

El helper real debe consultar también:

- HKLM 64-bit.
- HKLM 32-bit (`WOW64_32KEY`).
- HKCU/HKLM uninstall entries cuyo `DisplayName` sea Claude y cuyo publisher/ruta sean coherentes.

No debe confiar únicamente en `DisplayName`; hay que validar que el launcher exista. La firma Authenticode puede validarse durante descubrimiento o instalación, no en cada sondeo de procesos. Un chequeo rígido del texto exacto del certificado podría romperse cuando Anthropic renueve su certificado, por lo que conviene usar `WinVerifyTrust` y tratar el publisher como señal adicional.

#### Detector MSIX

Para MSIX debe exigirse que el paquete esté registrado para el usuario, no que exista su antiguo directorio de datos:

```go
installLocation := appxInstallLocation()
if installLocation != "" {
    executable := filepath.Join(installLocation, "app", processName)
    if regularFile(executable) {
        return windowsInstall{
            id:            "msix:" + windowsPackageFamily,
            kind:          installMSIX,
            dataRoot:      filepath.Join(local, "Packages", windowsPackageFamily, "LocalCache", "Roaming", "Claude"),
            launchTarget:  "shell:AppsFolder\\" + windowsAUMID,
            processRoot:   installLocation,
            packageFamily: windowsPackageFamily,
            aumid:          windowsAUMID,
        }, true
    }
}
```

Idealmente AUMID y Application ID se obtienen del manifest del paquete y se cachean; hardcodearlos debe ser fallback. Para un MSIX provisionado en toda la máquina pero no registrado para el usuario actual, `Get-AppxPackage` puede no devolverlo y tampoco sería lanzable por ese usuario todavía. No debe tratarse como instalación operativa.

#### Rutas Win32 conocidas

Como fallback, revisar al menos:

```text
%LOCALAPPDATA%\AnthropicClaude\claude.exe
%LOCALAPPDATA%\Programs\Claude\Claude.exe
%LOCALAPPDATA%\Claude\Claude.exe
%ProgramFiles%\Claude\Claude.exe
%ProgramFiles%\Anthropic\Claude\Claude.exe
%ProgramFiles(x86)%\Claude\Claude.exe
```

Las rutas deben obtenerse de Known Folders, registro o variables de entorno; nunca asumir `C:\Users` ni una arquitectura concreta. Esto cubre Windows x64/arm64, perfiles redirigidos, nombres con espacios y cuentas no administradoras.

### D3. Matching correcto de procesos Squirrel

Ubicación propuesta: `desktopProcesses()` en `internal/platform/windows.go`.

```go
func pathWithin(path, root string) bool {
    path = normalizeWindowsPath(path)
    root = normalizeWindowsPath(root)
    rel, err := filepath.Rel(root, path)
    if err != nil || rel == ".." {
        return false
    }
    return !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

func (install windowsInstall) ownsDesktopProcess(path string) bool {
    if !strings.EqualFold(filepath.Base(path), processName) {
        return false
    }

    switch install.kind {
    case installSquirrel:
        if !pathWithin(path, install.processRoot) {
            return false
        }
        versionDir := filepath.Base(filepath.Dir(path))
        return strings.HasPrefix(strings.ToLower(versionDir), "app-")
    case installMSIX:
        return pathWithin(path, install.processRoot)
    case installPortable, installWin32:
        return normalizeWindowsPath(path) == normalizeWindowsPath(install.launchTarget)
    default:
        return false
    }
}
```

Con esto se reconocen automáticamente `app-1.20186.1`, `app-1.20187.0`, etc., sin recompilar tras cada actualización, y se excluye Claude Code porque está fuera de `processRoot`.

En MSIX tampoco se debería aceptar cualquier proceso llamado `Claude.exe`, como hace actualmente `w.msix || ...`. Debe comprobarse que la imagen esté dentro del `InstallLocation` del paquete.

### D4. Elección segura cuando hay varias instalaciones

Una máquina puede tener simultáneamente:

- Squirrel individual.
- MSIX empresarial.
- Portable del switcher.
- Restos de instalaciones desinstaladas.

Reglas propuestas:

1. Enumerar todos los candidatos válidos.
2. Asociar cada proceso `Claude.exe` a un candidato por ruta.
3. Si exactamente una instalación reconocida está abierta, usar esa.
4. Si ninguna está abierta, usar la elección persistida del usuario si sigue válida.
5. Si no existe elección, preferir una instalación registrada y firmada sobre un portable.
6. Si hay dos instalaciones oficiales igualmente válidas con diferentes data roots, pedir una elección una sola vez y persistirla.
7. Si hay dos instalaciones abiertas con diferentes data roots, detener el swap y pedir cerrar una; no adivinar.

Config sugerida, sin tokens:

```json
{
  "preferred_windows_install": "squirrel:c:\\users\\...\\appdata\\local\\anthropicclaude"
}
```

El portable `ClaudeChatOnly` debe quedar como fallback o elección explícita, no tener prioridad sobre una instalación oficial registrada.

### D5. Instalaciones distintas que comparten el mismo data root

Squirrel y el portable actual usan `%APPDATA%\Claude`. Aunque el switcher elija Squirrel, antes de mutar ese directorio debe cerrar **todas las instancias de Claude Desktop reconocidas que usan el mismo data root**, no solo las del launcher elegido.

No debe cerrar Claude Code. La relación correcta es:

```text
proceso -> instalación reconocida -> dataRoot
```

Si dos instalaciones con el mismo `dataRoot` están abiertas, ambas deben cerrarse y solo debe relanzarse la instalación preferida. Esto evita que una instancia superviviente vuelva a escribir Cookies después del restore.

MSIX usa un data root diferente. Los perfiles no deberían moverse silenciosamente entre raíces de instalaciones distintas hasta confirmar que Cookies cifradas y Local Storage son compatibles entre esos contextos. Como mínimo, guardar el `installID`/tipo de origen en la metadata del perfil y validar antes de restaurar.

### D6. Caché e invalidación frente a actualizaciones

Squirrel cambia el directorio `app-<versión>` al actualizarse, pero mantiene estable `%LOCALAPPDATA%\AnthropicClaude\claude.exe` y el registro de uninstall. Por eso la caché debe guardar la raíz y launcher, no el executable versionado.

Invalidar y redetectar cuando:

- El launcher ya no existe.
- Un launch falla.
- El proceso iniciado no pertenece al candidato esperado.
- Vence el TTL.
- El usuario selecciona “volver a detectar Claude”.

El auto-refresh del tray no debe ejecutar PowerShell cada diez segundos. Puede consultar la caché y hacer redetección completa únicamente bajo esas condiciones.

### D7. Pruebas específicas de instalación

Agregar a `internal/platform/windows_test.go`:

- Squirrel registrada en HKCU devuelve launcher estable y data root roaming.
- El proceso `app-1.20186.1\claude.exe` pertenece a la instalación Squirrel.
- Tras actualizar a `app-1.20187.0`, el mismo detector sigue reconociéndolo.
- `%APPDATA%\Claude\claude-code\...\claude.exe` nunca se reconoce como Desktop.
- Squirrel registrada + portable existente: gana Squirrel salvo preferencia explícita o portable ya activo.
- Squirrel abierta + portable abierto con el mismo data root: ambas instancias se cierran antes del restore.
- MSIX registrada + directorio Squirrel residual: gana la instalación válida/preferida.
- Directorio MSIX residual sin paquete: no se considera instalado.
- MSIX provisionada pero no registrada para el usuario: no se considera lanzable.
- HKLM 64/32 y perfil Windows no estándar.
- Rutas con espacios, Unicode y Windows arm64.
- Dos instalaciones oficiales activas con diferentes data roots: el swap se niega de forma segura.

### D8. Criterios de aceptación para esta máquina

- El detector debe devolver `installSquirrel` y `%LOCALAPPDATA%\AnthropicClaude\claude.exe`, no `ClaudeChatOnly`.
- `IsInstalled()` debe devolver true aunque no exista AppX.
- `IsRunning()` debe detectar los procesos bajo `app-1.20186.1`.
- `KillApp()` debe cerrar esos procesos y preservar el proceso de Claude Code.
- `LaunchApp()` debe usar el launcher estable y reconocer el hijo versionado.
- `AppDataPath()` debe devolver `%APPDATA%\Claude` y `CookiesPath()` debe elegir `Network\Cookies`.
- Una actualización que cambie `app-<versión>` no debe requerir recompilar ni editar rutas.
- La presencia del portable no debe desviar automáticamente el switcher de la instalación registrada.

## Política de backups sin contraseña visible

### Objetivo de experiencia

Antes de crear un backup, el usuario debe elegir el nivel de portabilidad:

```text
¿Cómo quieres guardar el backup?

○ Proteger en este equipo
  No pide contraseña. Solo se puede abrir con este usuario de Windows.

○ Backup cifrado portable
  Pide una contraseña. Se puede importar en otro equipo o usuario.
```

La primera opción debe estar seleccionada por defecto. Nunca debe existir una opción de backup sin protección.

### Modo “Proteger en este equipo”

Debe cifrar el archivo usando Windows DPAPI con el usuario actual. Así:

- No se pide ni se muestra una contraseña.
- Reinstalar Windows Claude Swap no elimina la capacidad de importar el backup.
- Funciona mientras se conserve el mismo perfil de usuario de Windows.
- El archivo no sirve por sí solo en otro PC o en otra cuenta de Windows.
- La interfaz debe advertir claramente esa limitación.

Ubicación sugerida: `internal/profile/backup_windows.go`.

Esquema de API, pendiente de adaptar a las llamadas disponibles en `golang.org/x/sys/windows`:

```go
func encryptLocalBackup(archive []byte) ([]byte, error) {
    return dpapiProtectCurrentUser(archive)
}

func decryptLocalBackup(data []byte) ([]byte, error) {
    return dpapiUnprotectCurrentUser(data)
}
```

La protección debe usar el alcance del usuario actual, no un secreto guardado en el archivo. El wrapper debe usar `CryptProtectData` y `CryptUnprotectData` con la opción de no mostrar UI del sistema.

El formato debe guardar un identificador de protección, por ejemplo:

```json
{
  "format_version": 2,
  "protection": "windows-user-dpapi",
  "description": "Claude Desktop Swap backup"
}
```

No se deben guardar tokens, contraseñas ni claves DPAPI en ese manifest.

### Modo “Backup cifrado portable”

Debe conservar el flujo actual de contraseña:

- Contraseña solicitada mediante ventana nativa.
- Derivación con scrypt.
- Cifrado AES-GCM.
- Salt y nonce aleatorios.
- Importación posible en otro PC si se conoce la contraseña.

El código actual siempre exige contraseña en `cmd/backup.go` y usa scrypt en `internal/profile/backup.go`. La propuesta es conservar esa compatibilidad para backups existentes y añadir el nuevo modo DPAPI como formato alternativo.

Los backups antiguos con contraseña deben seguir importándose aunque el modo local pase a ser el predeterminado.

### Integración en tray y CLI

En `cmd/tray_windows.go`, la acción “Exportar backup cifrado...” debe abrir primero el selector nativo de modo. Después:

```go
mode, err := chooseBackupModeNative()
if err != nil {
    return err
}

switch mode {
case backupLocalWindows:
    return store.ExportLocalWindows(path)
case backupPortablePassword:
    password, err := nativeSecretPrompt("Contraseña del backup")
    if err != nil {
        return err
    }
    return store.Export(path, password)
default:
    return errBackupModeCancelled
}
```

Para el CLI, que no tiene el mismo flujo visual, se pueden añadir opciones explícitas:

```text
claude-desktop-swap export --local <archivo>
claude-desktop-swap export --portable <archivo>
claude-desktop-swap import <archivo>
```

En `import`, el programa debe leer el tipo de protección del archivo:

- DPAPI: intentar abrirlo automáticamente con el usuario de Windows.
- Password: mostrar la ventana nativa para pedir contraseña.
- Tipo desconocido: rechazarlo sin tocar perfiles existentes.

La importación debe seguir siendo transaccional: si la contraseña es incorrecta o DPAPI no puede descifrar, no se modifica ninguna cuenta.

### Regla global para ventanas y overlays

Todas las ventanas creadas por Windows Claude Swap deben ser nativas y ejecutarse dentro del proceso del tray. No se deben usar PowerShell, WPF, `Microsoft.VisualBasic.Interaction`, `InputBox` ni procesos externos para mostrar UI.

Esto incluye:

- Overlay de cambio de cuenta.
- Overlay de agregar cuenta.
- Overlay de éxito.
- Confirmación de eliminar cuenta.
- Nombre de nueva cuenta.
- Selector de modo de backup.
- Entrada de contraseña del backup portable.
- Mensajes de error y confirmación.
- Selección de archivo para exportar/importar.

Implementación recomendada:

- `CreateWindowExW` para formularios simples y overlays.
- `IFileDialog`/Common Item Dialog para seleccionar archivos.
- `MessageBoxW` o un diálogo Win32 propio para confirmaciones sencillas.
- El patrón ya usado en `cmd/delete_overlay_windows.go` como base del resto de formularios.
- Una ventana de overlay reutilizable, oculta cuando no se usa.
- Iconos cargados desde recursos o desde el ejecutable detectado, sin ejecutar `Get-AppxPackage` desde la UI.

PowerShell podría conservarse únicamente como fallback oculto de detección de MSIX si no existe una API nativa equivalente, con caché e invalidación. No debe estar en el camino normal de exportar, importar, agregar, eliminar o cambiar cuenta.

### Flujo nativo esperado para exportar

```text
clic en Exportar backup
  -> diálogo Win32 de modo
  -> selector nativo de archivo
  -> si es local: DPAPI sin contraseña
  -> si es portable: diálogo nativo de contraseña
  -> backup cifrado y confirmado
```

### Pruebas adicionales de backup y UI

Agregar pruebas para:

- Exportar local sin mostrar prompt de contraseña.
- Importar local con el mismo usuario de Windows.
- Reinstalar la aplicación y volver a importar localmente.
- Rechazar correctamente un backup DPAPI en otro usuario/equipo.
- Exportar e importar portable con contraseña.
- Mantener compatibilidad con backups password-encrypted existentes.
- Contraseña incorrecta sin modificar perfiles.
- Cancelar cada diálogo sin iniciar operaciones parciales.
- Ninguna operación de UI inicia `powershell.exe`.
- Overlay nativo visible y ocultable sin proceso secundario.
- File dialog nativo en rutas con espacios, Unicode y OneDrive.

### Criterios de aceptación

- El usuario elige entre “Proteger en este equipo” y “Backup cifrado portable”.
- El modo local no pide contraseña y sigue cifrado.
- El modo portable sigue permitiendo trasladar el archivo a otro PC.
- Los tokens y perfiles nunca quedan en texto plano.
- Se conservan los backups antiguos protegidos con contraseña.
- Todas las ventanas y overlays del producto son nativos.
- El flujo normal no crea procesos PowerShell para mostrar UI.
- El overlay de cambio aparece instantáneamente y se reutiliza entre operaciones.
- Cancelar un diálogo no deja locks, overlays ni operaciones pendientes.

## Hallazgos de lógica

### C1. El fast path confía en un marcador potencialmente obsoleto

Se ignora el error de `store.Current()` y luego se usa `current == name` para omitir el restore:

```go
current, _ := store.Current()

if current == name {
    return p.LaunchApp()
}
```

Ubicación: `cmd/use.go`, alrededor de las líneas 112–126.

El marcador solo indica qué restore terminó por última vez. No demuestra que las Cookies vivas todavía correspondan a ese perfil. Puede estar obsoleto si:

- Las Cookies fueron eliminadas o reemplazadas.
- Claude cerró sesión por su cuenta.
- Una operación anterior terminó parcialmente.
- El usuario abrió otra instalación o modificó los datos fuera del switcher.

Consecuencia: se puede iniciar Claude con una sesión vacía o equivocada aunque el menú marque la cuenta correcta.

Hay además una contradicción en `cmd/use_test.go`: `TestSwitchProfileCanRestoreCurrentProfileWhenLiveCookiesAreMissing` espera `stop, launch`, pero no espera `restore`. El nombre del test describe el comportamiento seguro y su expectativa codifica lo contrario.

#### Solución propuesta

Usar `MatchLiveAt()` como prueba del fast path. El marcador puede ser un fallback informativo, no una autoridad.

Ejemplo para `cmd/use.go`:

```go
type liveMatchingStore interface {
    MatchLiveAt(string) (string, profile.Health)
}

liveName, liveHealth := "", profile.HealthUnknown
if matcher, ok := store.(liveMatchingStore); ok {
    liveName, liveHealth = matcher.MatchLiveAt(platform.CookiesPath(appData))
}

if liveName == name && liveHealth == profile.HealthUsable {
    if wasRunning {
        return nil
    }
    return p.LaunchApp()
}
```

Si la lectura falla mientras Claude está abierto, no debe asumirse que el perfil coincide. Se puede cerrar Claude y repetir una sola vez la detección antes de mutar datos.

### C2. Se pierde el último estado si Claude ya estaba cerrado

El checkpoint se ejecuta solamente con:

```go
if wasRunning && current != "" {
    // checkpoint
}
```

Ubicación: `cmd/use.go`, alrededor de la línea 128.

Cerrar Claude normalmente hace que Electron termine de escribir Cookies y LevelDB. Es precisamente después de estar cerrado cuando el snapshot puede actualizarse con seguridad. Omitir el checkpoint puede descartar:

- Renovaciones del token de sesión.
- Cambios en `routingHint` o `lastActiveOrg`.
- Estado de confianza del dispositivo almacenado en LevelDB.
- Cualquier cambio ocurrido desde el último checkpoint del perfil.

El restore siguiente sobrescribe esos datos vivos con otra cuenta.

#### Solución propuesta

Checkpoint del perfil vivo conocido independientemente de `wasRunning`:

```go
if outgoing != "" && outgoing != name {
    if err := store.CheckpointAt(outgoing, appData, cookies); err != nil {
        return fmt.Errorf("checkpoint outgoing profile: %w", err)
    }
}
```

`wasRunning` solo debe decidir si hay que cerrar o volver a abrir Claude, no si se preserva la sesión.

Si existe una sesión usable pero `MatchLiveAt()` no reconoce ninguna cuenta guardada, el restore destructivo debe detenerse o guardar primero un perfil de recuperación. No debe adjudicarse esa sesión al valor de `current` sin comprobarla.

### C3. El rollback puede dejar una sesión híbrida

`RestoreAt()` conserva `.Cookies.rollback`, pero `restoreVolatile()` elimina el LevelDB vivo antes de copiar el destino:

```go
os.RemoveAll(liveLevelDB)
copyDir(snapshot, liveLevelDB)
```

Si falla la copia, `setLastUsed()` o `SetCurrent()`, el rollback restaura Cookies, pero no Local Storage, IndexedDB ni Session Storage.

Consecuencia: Cookies de la cuenta anterior con device-trust de la cuenta destino, o un LevelDB parcial. Esto puede causar login repetido, validación elevada o una sesión aparentemente corrupta.

#### Solución propuesta

Preparar todos los datos destino antes de tocar los vivos y rotar directorios por rename. El staging y rollback deben estar en el mismo volumen.

Esquema para `internal/profile/store.go`:

```go
liveLevelDB := filepath.Join(appDataPath, localStorageDir, leveldbDir)
stageLevelDB := liveLevelDB + ".stage"
backupLevelDB := liveLevelDB + ".rollback"

_ = os.RemoveAll(stageLevelDB)
_ = os.RemoveAll(backupLevelDB)

snapshot := filepath.Join(s.profileDir(name), localStorageDir, leveldbDir)
if err := copyDir(snapshot, stageLevelDB); err != nil {
    return fmt.Errorf("stage Local Storage: %w", err)
}

hadLiveLevelDB := pathExists(liveLevelDB)
if hadLiveLevelDB {
    if err := os.Rename(liveLevelDB, backupLevelDB); err != nil {
        return err
    }
}

rollbackLevelDB := func() {
    _ = os.RemoveAll(liveLevelDB)
    if hadLiveLevelDB {
        _ = os.Rename(backupLevelDB, liveLevelDB)
    }
}

if err := os.Rename(stageLevelDB, liveLevelDB); err != nil {
    rollbackLevelDB()
    return err
}
```

La transacción completa debería incluir:

- Cookies.
- Cookies WAL/SHM/journal si existen.
- `Local Storage/leveldb`.
- IndexedDB y Session Storage que actualmente se eliminan.
- `current` y `LastUsed`, actualizados únicamente después del commit de archivos.

En caso de fallo se restauran todos los elementos; en caso de éxito se borran los backups.

### C4. Un error puede dejar Claude cerrado

El flujo cierra Claude antes de checkpoint y restore. Si cualquiera falla, retorna inmediatamente. No existe recuperación que vuelva a abrir el estado anterior.

Ubicación: `cmd/use.go`, después de `p.KillApp()`.

#### Solución propuesta

Separar claramente tres estados:

- `stopped`: Claude fue cerrado por esta operación.
- `targetCommitted`: el restore destino terminó completamente.
- `wasRunning`: Claude estaba abierto al inicio.

Boceto para `cmd/use.go`:

```go
func switchProfileWith(...) (err error) {
    wasRunning, err := p.IsRunning()
    if err != nil {
        return err
    }

    stopped := false
    targetCommitted := false
    defer func() {
        if err == nil || !wasRunning || !stopped || targetCommitted {
            return
        }
        if launchErr := p.LaunchApp(); launchErr != nil {
            err = errors.Join(err, fmt.Errorf("relaunch previous session: %w", launchErr))
        }
    }()

    if wasRunning {
        if err = p.KillApp(); err != nil {
            return err
        }
        stopped = true
    }

    // checkpoint y restore transaccional
    targetCommitted = true
    return p.LaunchApp()
}
```

Esto depende de que `RestoreAt()` garantice rollback completo. Después de un commit exitoso, un fallo al iniciar Claude debe reintentar el destino, no intentar volver a la cuenta anterior.

### C5. Detección falsa de MSIX

La condición actual acepta un directorio de datos residual como prueba de instalación:

```go
if _, err := os.Stat(msixRoot); err == nil || packageInstalled(msixExe) {
    return msixRoot, msixExe, true
}
```

Un uninstall puede dejar `LocalCache`. En ese caso se selecciona MSIX aunque no exista AUMID registrado, y se ignoran instalaciones válidas Squirrel, Win32 o portable.

#### Solución propuesta

Exigir un paquete/ejecutable registrado válido:

```go
installLocation := appxInstallLocation()
if installLocation != "" {
    msixExe := filepath.Join(installLocation, "app", processName)
    if packageInstalled(msixExe) {
        return msixRoot, msixExe, true
    }
}
```

La existencia de `msixRoot` puede servir para encontrar datos, nunca para declarar la aplicación instalada.

### C6. Dos clics pueden cruzar el estado visual

`watchAccount()` deshabilita solamente el item presionado. Otro item puede iniciar una segunda goroutine. `operation.lock` evita dos mutaciones simultáneas, pero no evita:

- Un error de “otra operación activa” durante un switch válido.
- Que el segundo mensaje reemplace temporalmente el estado del primero.
- Que `autoRefresh()` vuelva a habilitar items durante una operación larga.

#### Solución propuesta

Agregar un estado `switching` protegido por `trayState.mu`, deshabilitar todas las cuentas y hacer que `autoRefresh()` se abstenga de modificar el menú.

Ejemplo para `cmd/tray_windows.go`:

```go
type trayState struct {
    mu        sync.Mutex
    switching bool
    // campos actuales
}

func (s *trayState) beginSwitch() bool {
    s.mu.Lock()
    defer s.mu.Unlock()
    if s.switching || s.workflow != nil {
        return false
    }
    s.switching = true
    for _, item := range s.items {
        item.Disable()
    }
    return true
}

func (s *trayState) endSwitch() {
    s.mu.Lock()
    s.switching = false
    for _, item := range s.items {
        if s.claudeInstalled {
            item.Enable()
        }
    }
    s.mu.Unlock()
}
```

No se debe llamar a `disableAccounts()` mientras ya se posee `s.mu`, porque ese método adquiere el mismo mutex.

## Hallazgos de rendimiento

### P1. PowerShell está en el camino crítico

`current()` llama `detectWindowsInstall()` y esta llama `appxInstallLocation()` en cada ocasión. El tray también ejecuta `platform.Installed()` desde `loadAccounts()` cada diez segundos.

#### Solución propuesta

Cachear la detección con TTL o invalidación explícita. Un TTL evita que sea necesario reiniciar el tray después de instalar o desinstalar Claude.

Ejemplo para `internal/platform/windows.go`:

```go
type installResult struct {
    root       string
    executable string
    msix       bool
    checkedAt  time.Time
}

var installState struct {
    sync.Mutex
    result installResult
}

func detectWindowsInstallCached() (string, string, bool) {
    installState.Lock()
    defer installState.Unlock()

    if time.Since(installState.result.checkedAt) < 5*time.Minute {
        r := installState.result
        return r.root, r.executable, r.msix
    }

    root, executable, msix := detectWindowsInstallUncached()
    installState.result = installResult{
        root: root, executable: executable, msix: msix, checkedAt: time.Now(),
    }
    return root, executable, msix
}
```

Antes de ejecutar PowerShell conviene consultar fuentes Win32 baratas: registro Squirrel, `App Paths` y launchers oficiales conocidos. El portable administrado debe ser fallback o preferencia explícita, no ganar por aparecer primero en una lista.

### P2. El overlay crea PowerShell/WPF cada vez

`cmd/switch_overlay_windows.go` crea un proceso PowerShell, carga cuatro assemblies y consulta AppX para extraer el icono. En la instalación portable, esa extracción puede fallar y ocultar el spinner.

#### Solución recomendada

Reemplazarlo por una ventana Win32 dentro del proceso del tray, siguiendo el enfoque nativo ya utilizado por el diálogo de eliminación. La ventana puede crearse una vez y alternarse entre visible/oculta.

Esqueleto para `cmd/switch_overlay_windows.go`:

```go
var switchPostMessage = windows.NewLazySystemDLL("user32.dll").NewProc("PostMessageW")

const switchWMClose = 0x0010

type switchOverlay struct {
    hwnd  uintptr
    ready chan struct{}
    done  chan struct{}
}

func startSwitchOverlay() *switchOverlay {
    overlay := &switchOverlay{
        ready: make(chan struct{}),
        done:  make(chan struct{}),
    }
    go overlay.runWindowLoop()
    <-overlay.ready
    return overlay
}

func (o *switchOverlay) Close() {
    if o == nil || o.hwnd == 0 {
        return
    }
    switchPostMessage.Call(o.hwnd, switchWMClose, 0, 0)
    <-o.done
}
```

Detalles a conservar:

- `WS_EX_TOPMOST | WS_EX_LAYERED | WS_EX_TOOLWINDOW`.
- Fondo gris con alpha.
- Form centrado en el monitor activo, no en el escritorio virtual completo.
- Icono cargado desde el ejecutable ya detectado, sin consultar AppX.
- Timer Win32 para rotación o animación simple.
- `PostMessage(WM_CLOSE)` en vez de matar un proceso.
- Cierre automático si el tray termina inesperadamente, porque la ventana pertenece al mismo proceso.

### P3. El overlay cubre trabajo que no requiere bloqueo visual

Actualmente se crea antes de obtener `AppDataPath`, validar el destino y detectar la instalación.

#### Solución propuesta

Hacer el preflight sin overlay:

```text
adquirir lock
detectar plataforma cacheada
validar destino
determinar si es no-op
deshabilitar menú
mostrar overlay
cerrar/checkpoint/restore/iniciar
ocultar overlay
```

Así los errores de configuración no producen un overlay lento o un destello innecesario.

### P4. Se valida dos veces el perfil destino

`switchProfileWith()` llama `store.Inspect(name)` y `RestoreAt()` vuelve a llamar `s.Inspect(name)`.

No debería eliminarse el preflight, porque evita cerrar Claude por un destino corrupto. Opciones:

1. Crear `PrepareRestore()` que devuelve un token validado y `RestorePrepared()` lo consume.
2. Añadir una ruta interna `restoreAt(..., alreadyValidated bool)`.
3. Mantenerlo hasta medirlo si su costo es pequeño; es una optimización de prioridad media.

Ejemplo mínimo dentro de `internal/profile/store.go`:

```go
func (s *Store) RestorePreparedAt(name, appDataPath, live string) error {
    return s.restoreAt(name, appDataPath, live, false)
}

func (s *Store) RestoreAt(name, appDataPath, live string) error {
    return s.restoreAt(name, appDataPath, live, true)
}
```

El agente revisor debe valorar si la interfaz adicional compensa el ahorro.

### P5. Los archivos se sincronizan dos veces durante checkpoint

`copyFile()` ejecuta `out.Sync()` por cada archivo. Después `CheckpointAt()` llama `syncTree(stage)`, que vuelve a abrir y sincronizar cada archivo. En Windows `syncDir()` ya es no-op.

#### Solución de bajo riesgo

Evitar la segunda pasada en Windows, manteniendo el `Sync()` de cada archivo copiado:

```go
if runtime.GOOS != "windows" {
    if err := syncTree(stage); err != nil {
        return err
    }
}
```

Una opción más agresiva sería no hacer `Sync()` individual del LevelDB restaurado y confiar en que el snapshot original permanece intacto. No se recomienda sin pruebas de corte abrupto o fallo simulado.

### P6. Seleccionar la cuenta activa reinicia Claude

Incluso si el perfil vivo ya coincide y Claude está abierto, se ejecuta `KillApp()` y `LaunchApp()`.

#### Solución propuesta

- Cuenta viva coincidente + Claude abierto: no-op o traer la ventana al frente.
- Cuenta viva coincidente + Claude cerrado: iniciar Claude sin restore.
- Marcador coincidente pero sesión viva ausente/diferente: restaurar.

Este cambio hace prácticamente instantáneo el caso más frecuente de clic accidental sobre la cuenta activa.

### P7. `LaunchApp()` mezcla inicio y verificación

La función inicia Claude y espera cinco sondeos positivos del proceso. Eso agrega al menos unos 400 ms después de detectar el primer proceso, pero no demuestra que exista una ventana utilizable.

#### Solución propuesta

Separar las responsabilidades en `internal/platform/platform.go`:

```go
type Platform interface {
    // métodos actuales
    LaunchApp() error
    WaitForMainWindow(context.Context) error
}
```

En Windows, `LaunchApp()` solo debe devolver errores inmediatos de `ShellExecute` o `exec.Start`. El tray cierra el overlay después del inicio y verifica la ventana en segundo plano:

```go
if err := p.LaunchApp(); err != nil {
    return err
}
overlay.Close()

go func() {
    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()
    if err := p.WaitForMainWindow(ctx); err != nil {
        s.setStatus("Claude no mostró una ventana; reintentando...")
        _ = p.LaunchApp()
    }
}()
```

La comprobación de ventana debería ser nativa (`EnumWindows`, PID y `IsWindowVisible`) para no ejecutar PowerShell cada 100 ms.

Esto separa dos tiempos distintos:

- Swap completado y proceso solicitado: determina la duración del overlay.
- Electron listo para interacción: se monitoriza sin bloquear toda la pantalla.

### P8. Sondeo y cierre de procesos

`KillApp()` consulta procesos cada 100 ms y no fuerza el cierre hasta aproximadamente un segundo. Es razonable como estrategia segura, pero se puede reducir jitter esperando handles de proceso con `WaitForSingleObject` después de solicitar el cierre.

No debería priorizarse antes de medir; el ahorro probable es menor que eliminar PowerShell y sincronizaciones duplicadas.

## Flujo recomendado

```text
1. adquirir lock
2. obtener plataforma desde caché
3. validar perfil destino
4. detectar sesión viva
5. si destino ya está vivo:
     - abierto: no-op/activar ventana
     - cerrado: iniciar y terminar
6. bloquear todos los items del tray
7. mostrar overlay nativo
8. cerrar Claude solo si estaba abierto
9. repetir detección viva con Claude cerrado
10. si hay cuenta saliente conocida, checkpoint siempre
11. preparar restore completo en staging
12. commit atómico de Cookies + Local Storage
13. actualizar current/LastUsed
14. iniciar Claude
15. cerrar overlay al aceptar el inicio
16. verificar ventana de manera asíncrona
17. desbloquear items y actualizar estado
```

## Instrumentación recomendada antes de optimizar

Agregar temporalmente mediciones por fase, sin registrar valores de cookies ni rutas sensibles:

```go
type switchTimings struct {
    Preflight   time.Duration
    Stop        time.Duration
    Checkpoint  time.Duration
    Restore     time.Duration
    Launch      time.Duration
}
```

Ejemplo local para `cmd/use.go`:

```go
started := time.Now()
if err := p.KillApp(); err != nil {
    return err
}
timings.Stop = time.Since(started)
```

La salida debe activarse solo con una opción como `--debug-timing` o variable de entorno. No debe imprimir cookies, tokens, emails ni contenido de perfil.

## Pruebas que faltan o deben corregirse

### `cmd/use_test.go`

- Marcador destino + Cookies vivas ausentes: debe restaurar destino.
- Marcador destino + sesión viva de otra cuenta: no debe tomar fast path.
- Claude cerrado + cuenta saliente viva: debe checkpointar antes de restore.
- Destino ya vivo y Claude abierto: no debe cerrar ni relanzar.
- Fallo de checkpoint con Claude inicialmente abierto: debe relanzar la sesión anterior.
- Fallo de restore: debe mantener `current` anterior y relanzarlo.
- Fallo de launch después del commit: debe reintentar el destino sin volver al anterior.

### `internal/profile/store_test.go`

- Fallo después de reemplazar Cookies: rollback de Cookies y LevelDB.
- Fallo durante copia de LevelDB: ningún directorio parcial queda como live.
- Fallo en `SetCurrent`: archivos vivos y metadata regresan al estado anterior.
- Restore exitoso: elimina todos los `.rollback` y `.stage`.
- Recuperación al iniciar después de una interrupción entre renames.

### `internal/platform/windows_test.go`

- `msixRoot` residual sin paquete no se considera instalación.
- Portable válida se detecta si MSIX no está registrado.
- La caché evita múltiples llamadas consecutivas a AppX.
- Invalidación/TTL detecta instalación o desinstalación posterior.
- `LaunchApp()` no espera cinco sondeos si la verificación se separa.
- `WaitForMainWindow()` no acepta procesos sin ventana visible.

### Tray

- Dos clics rápidos solo producen una operación.
- Todos los items quedan deshabilitados durante el switch.
- `autoRefresh()` no modifica items mientras `switching == true`.
- El overlay se cierra aunque checkpoint, restore o launch fallen.

## Criterios de aceptación

- La reinstalación Squirrel actual se detecta como instalación preferida aunque exista `ClaudeChatOnly`.
- Los procesos versionados de Squirrel se cierran sin cerrar Claude Code.
- MSIX, Squirrel, Win32 y portable se distinguen por identidad, launcher, process root y data root.
- Nunca se sobrescribe una sesión viva no identificada.
- Una cuenta usada y luego cerrada se checkpointa antes de cambiar.
- Un error de restore no mezcla Cookies y Local Storage.
- Si Claude estaba abierto y el swap falla antes del commit, vuelve a abrirse con la cuenta anterior.
- Seleccionar la cuenta ya activa no reinicia Claude.
- No se ejecuta PowerShell en el camino normal del switch.
- No aparecen dos overlays ni dos operaciones concurrentes.
- El overlay termina al aceptar el inicio del proceso; la ventana se monitoriza en segundo plano.
- Objetivo inicial medible: overlay menor de 2 segundos en mediana y menor de 3 segundos en p95 en esta máquina.
- El tiempo objetivo no se logra sacrificando checkpoint o rollback.

## Orden sugerido de implementación

### Fase 1: corrección

1. Implementar el modelo y descubrimiento multi-instalación D1–D6.
2. Detectar Squirrel y sus procesos versionados sin afectar Claude Code.
3. Validar sesión viva y eliminar la confianza ciega en `current`.
4. Restaurar el checkpoint cuando Claude está cerrado.
5. Hacer transaccional Cookies + Local Storage.
6. Recuperar/reabrir la sesión anterior cuando falle una operación previa al commit.
7. Corregir detección MSIX residual.

### Fase 2: mejoras rápidas de rendimiento

1. Cachear detección de instalación.
2. Evitar `syncTree()` duplicado en Windows.
3. Implementar fast path real para cuenta activa.
4. Mover preflight fuera del tiempo visible del overlay.
5. Medir de nuevo.

### Fase 3: UX y arranque

1. Reemplazar overlay PowerShell por Win32 nativo y reutilizable.
2. Reemplazar prompts, confirmaciones, selectores de archivo y contraseñas por ventanas nativas.
3. Añadir los modos de backup DPAPI local y contraseña portable.
4. Separar inicio de proceso y verificación de ventana.
5. Verificar ventana con APIs nativas y reintentar una sola vez.
6. Bloquear globalmente los items durante el switch.

### Fase 4: optimización adicional solo si sigue siendo necesaria

1. Manifest de cambios para no volver a copiar LevelDB sin cambios.
2. Hardlinks o block cloning de snapshots inmutables en NTFS, después de validar semántica y recuperación.
3. Espera por handles en vez de sondeo de procesos.

No se recomienda comenzar con hardlinks o swaps de directorios de perfiles completos: aumentan la complejidad y pueden comprometer backups/exportación si no se rediseña la noción de perfil activo.

## Preguntas para el agente revisor

1. ¿`MatchLiveAt()` es suficiente para identificar la cuenta saliente o debería existir un identificador de sesión más explícito?
2. ¿Conviene detenerse ante una sesión viva no reconocida o crear automáticamente un perfil de recuperación?
3. ¿La transacción por rename cubre correctamente Cookies, LevelDB, IndexedDB y Session Storage en Windows?
4. ¿Es aceptable cerrar el overlay al iniciar el proceso y verificar la ventana después, o debe mantenerse hasta ver una ventana real?
5. ¿La caché de instalación debería usar TTL, invalidación explícita o durar toda la vida del tray?
6. Si existen Squirrel, MSIX y portable, ¿la prioridad propuesta y la elección persistida son suficientes?
7. ¿Qué sincronizaciones pueden eliminarse en Windows sin reducir la recuperación ante corte abrupto?
8. ¿Debe el clic sobre la cuenta activa ser no-op o intentar traer Claude al frente?
9. ¿Los perfiles deben vincularse a `installID` o basta vincularlos a `dataRoot`?
10. ¿Debe una instalación abierta ganar siempre, o se debe bloquear cuando contradice la preferencia guardada?
