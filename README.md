# Windows Claude Swap

<p align="center">
  <img src="cmd/assets/windows-claude-swap-icon-v2.png" alt="Windows Claude Swap" width="180">
</p>

<p align="center">
  Cambia entre varias cuentas de Claude Desktop desde el icono del tray de Windows.
</p>

<p align="center">
  <a href="https://github.com/TinoA/claude-desktop-swap/releases/latest">Descargar para Windows</a>
  ·
  <a href="https://github.com/TinoA/claude-desktop-swap/issues">Reportar un problema</a>
</p>

Windows Claude Swap está pensado para algo muy concreto: guardar tus sesiones
de Claude Desktop y cambiar de cuenta sin tener que cerrar sesión manualmente
cada vez.

> Importante: esta aplicación es para **Claude Desktop en Windows**. No cambia
> ni cierra procesos de Claude Code.

## ¿Qué puedes hacer?

- Guardar varias cuentas con nombres fáciles de reconocer.
- Cambiar de cuenta desde el tray con un clic.
- Agregar una cuenta nueva con un inicio de sesión guiado.
- Guardar automáticamente una sesión renovada al cerrar Claude Desktop.
- Exportar e importar backups con contraseña o protegidos por Windows.
- Eliminar una cuenta guardada sin tocar las demás.
- Recibir avisos cuando exista una nueva versión publicada en GitHub.
- Usar overlays y avisos nativos de Windows durante las operaciones.

## Instalación rápida

1. Descarga el instalador desde la
   [última versión publicada](https://github.com/TinoA/claude-desktop-swap/releases/latest).
2. Elige el archivo adecuado:

   - `Windows-Claude-Swap-Setup-amd64.exe`: Windows normal de 64 bits.
   - `Windows-Claude-Swap-Setup-arm64.exe`: Windows sobre ARM.

3. Ejecuta el instalador.
4. Abre el icono de Windows Claude Swap en el área de notificaciones.

La instalación es por usuario y normalmente no necesita permisos de
administrador. También crea un desinstalador normal y puede iniciar el tray con
Windows.

## Cómo usarlo

### Cambiar de cuenta

Abre el menú del icono y entra en `Cuentas`. Selecciona el perfil que quieres
usar. El programa cerrará Claude Desktop de forma controlada, cambiará la
sesión y lo abrirá de nuevo.

### Agregar una cuenta

Selecciona `Agregar cuenta...`, escribe un nombre para reconocerla y completa
el inicio de sesión en Claude Desktop. Cuando la sesión esté lista, el perfil se
guarda y aparece automáticamente en la lista.

### Eliminar una cuenta

Selecciona `Eliminar cuenta...`, elige el perfil y confirma. Se eliminan sus
archivos locales, pero se conservan las demás cuentas. La cuenta que está
activa queda protegida mientras existan otros perfiles.

### Hacer un backup

Entra en `Backup` y elige una opción:

- `Con contraseña`: crea un archivo que puedes guardar o mover como archivo.
- `Sin contraseña`: lo protege usando tu usuario y equipo de Windows.

Para importar solo debes elegir `Importar backup`; el programa detecta
automáticamente si necesita contraseña.

Para un backup completo, Claude Desktop se detiene de forma controlada, se
actualiza el perfil activo y vuelve a iniciarse. Si la sesión no puede
comprobarse correctamente, el backup se cancela para evitar guardar datos
incompletos.

## ¿Qué se guarda?

Cada perfil conserva una copia de los datos necesarios para recuperar la sesión
en el mismo equipo:

- Cookies de Claude Desktop, incluyendo sus valores cifrados.
- Local Storage, IndexedDB y Session Storage.
- Estado del dispositivo y metadatos de la cuenta.

Los valores de cookies nunca se descifran, muestran ni escriben en logs.

La sincronización de chats depende de la cuenta de Claude. Windows Claude Swap
no copia conversaciones: al cambiar a una cuenta, Claude muestra el historial
que pertenece a esa cuenta en sus servidores.

## Verificación y límites importantes

Una sesión puede pedir verificación otra vez si Claude la expira, la revoca,
detecta un evento de seguridad o cambia sus requisitos. Ninguna aplicación local
puede garantizar que Anthropic nunca vuelva a solicitarla.

Los backups están pensados principalmente para recuperar cuentas en el mismo
equipo. Las cookies de Chromium están protegidas por la clave de Windows, por
lo que importar un backup en otro ordenador puede requerir iniciar sesión otra
vez.

## Ubicación de los perfiles

Los perfiles se guardan en:

```text
%USERPROFILE%\.claude-swap\profiles\<nombre>\
```

Desinstalar Windows Claude Swap conserva los perfiles y backups. Esto permite
reinstalar la aplicación sin perder las cuentas guardadas. Si quieres borrar
todo manualmente, elimina también `%USERPROFILE%\.claude-swap`.

No subas esa carpeta ni tus backups a GitHub: contienen sesiones protegidas y
datos privados.

## Si Claude Desktop no abre

Prueba este orden:

1. Comprueba que Claude Desktop esté instalado y actualizado.
2. Cierra cualquier instancia bloqueada de Claude Desktop.
3. Desde el tray, cambia a una cuenta guardada válida.
4. Si Claude pidió verificar la cuenta, completa el inicio de sesión y vuelve a
   cerrar Claude normalmente para que la sesión renovada se guarde.

Windows Claude Swap detecta instalaciones normales, Squirrel, MSIX y algunas
instalaciones portables de Claude Desktop.

## Ejecutar desde el código

Necesitas Go y Windows. Desde PowerShell:

```powershell
go test ./...
go vet ./...
go build -trimpath -o claude-desktop-swap.exe .
.\claude-desktop-swap.exe tray
```

El ejecutable creado localmente es independiente del instalador publicado.
Compilar no desinstala Claude Desktop ni elimina perfiles.

## Publicación y actualizaciones

Este repositorio es el fork de
[`FranCalveyra/claude-desktop-swap`](https://github.com/FranCalveyra/claude-desktop-swap).
La versión mantenida para Windows se publica en
[`TinoA/claude-desktop-swap`](https://github.com/TinoA/claude-desktop-swap).

Las versiones se construyen mediante GitHub Actions y publican:

- Archivos CLI para los sistemas compatibles.
- Instaladores Windows amd64 y arm64.
- Checksums para verificar las descargas.

El programa consulta las actualizaciones únicamente en el repositorio de este
fork. Los releases no incluyen cookies, tokens, perfiles ni backups personales.

## Licencia

MIT. Consulta [LICENSE](LICENSE).

Este proyecto está basado en y reconoce el trabajo de
[`FranCalveyra/claude-desktop-swap`](https://github.com/FranCalveyra/claude-desktop-swap).
