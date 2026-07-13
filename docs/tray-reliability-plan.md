# Plan: flujo de cuentas confiable desde el system tray

## Diagnóstico confirmado

1. `tray` ejecuta `add` como proceso hijo de una aplicación oculta. Ese hijo no
   tiene consola ni entrada estándar utilizable.
2. `add` ignora el error de `ReadString`; EOF se interpreta como si el usuario
   hubiera terminado el login. Claude se abre y se vuelve a cerrar de inmediato.
3. Al completar `add`, Claude no se vuelve a iniciar.
4. Los errores del proceso hijo se descartan, por lo que el tray no informa qué
   falló.
5. No existe exclusión mutua: pueden coexistir varios trays y operaciones
   `add`/`use`, cada una capaz de abrir o cerrar Claude.
6. `LaunchApp` no es idempotente y `KillApp` intenta terminar todos los procesos
   Electron repetidamente, en vez de controlar una sola transición de estado.
7. Si algo falla después de borrar la sesión viva, no hay rollback garantizado
   al perfil anterior.

## Experiencia objetivo

### Agregar cuenta

1. El usuario elige **Agregar cuenta** y escribe un nombre.
2. El sistema adquiere un lock global y desactiva acciones incompatibles.
3. Se guarda de forma verificable el perfil activo antes de tocar la sesión.
4. Se detiene Claude una sola vez, se prepara una sesión limpia y se abre Claude
   una sola vez.
5. El tray muestra `Esperando inicio de sesión: <nombre>` y una acción
   **Finalizar registro**. El login se realiza en la ventana de Claude; si Claude
   deriva a un navegador, ese comportamiento lo controla Claude.
6. Al pulsar **Finalizar registro**, se detiene Claude, se valida la nueva cookie,
   se guarda el perfil y se vuelve a abrir Claude con ese perfil activo.
7. Cancelar o fallar en cualquier punto restaura el perfil anterior y vuelve a
   abrir Claude una sola vez.

No se debe depender de `stdin` ni de una terminal para el flujo del tray.

## Cambios de implementación

### 1. Extraer un workflow compartido

- Crear un controlador `AddWorkflow` independiente de Cobra y del tray.
- Estados explícitos: `idle`, `snapshotting`, `preparing_login`,
  `waiting_for_login`, `saving`, `recovering`, `done`, `failed`.
- Separar `Begin(name)`, `Complete()` y `Cancel()`.
- Persistir la operación pendiente en `~/.claude-swap/pending.json` para poder
  recuperar después de un cierre inesperado.
- Guardar el nombre del perfil anterior antes de borrar cualquier dato.

### 2. Corregir el CLI

- Reutilizar `AddWorkflow` desde `add`.
- Tratar EOF como cancelación/error, nunca como confirmación.
- En éxito, relanzar Claude.
- En cualquier error posterior al wipe, restaurar el perfil anterior y relanzar.

### 3. Corregir el tray

- No crear un proceso `add` oculto.
- Invocar el workflow directamente y mostrar estado/errores en el menú.
- Añadir **Finalizar registro** y **Cancelar registro** cuando haya una operación
  pendiente.
- Desactivar `Agregar` y los switches mientras exista una operación.
- Añadir un log no sensible en `~/.claude-swap/tray.log`.
- No ignorar errores con `_ =`; todos deben llegar a la UI.

### 4. Evitar concurrencia y loops

- Mutex nombrado de Windows para permitir una sola instancia del tray.
- Lock global de operación compartido por tray y CLI.
- `LaunchApp` debe comprobar primero `IsRunning`, lanzar una vez y esperar a que
  aparezca el proceso principal.
- Detectar procesos raíz de Claude Desktop por PID/ParentPID y cerrar cada árbol
  una sola vez; Claude Code debe seguir excluido por ruta exacta.
- Timeout con un único fallback forzado, sin bucles de `taskkill` sobre todos los
  procesos Electron.

### 5. Recuperación transaccional

- No ejecutar wipe hasta confirmar que el checkpoint anterior es usable.
- Si la nueva sesión no es usable, mantener `waiting_for_login`; no guardar ni
  sobrescribir perfiles.
- Si se cancela o vence el timeout, restaurar el perfil anterior.
- Al iniciar tray/CLI, detectar `pending.json` y ofrecer **Continuar** o
  **Restaurar cuenta anterior**.

## Pruebas necesarias

### Unitarias

- Orden completo del alta exitosa.
- EOF/cancelación restaura y relanza.
- Login incompleto no crea perfil.
- Fallos de stop, wipe, checkpoint, restore y launch recuperan el estado válido.
- Dos operaciones simultáneas son rechazadas.
- Una segunda instancia del tray es rechazada.
- `LaunchApp` no lanza si Claude ya está activo.
- Detección y cierre del proceso raíz no incluye Claude Code.

### Integración sintética

- Dos perfiles SQLite temporales con cambio A → B → A.
- Interrupción en cada etapa y recuperación desde `pending.json`.
- Verificar que nunca se sobrescribe un perfil usable con una sesión missing,
  expired o unknown.

### Validación manual controlada

1. Crear backup de Cookies y perfiles.
2. Iniciar una sola instancia del tray.
3. Agregar una segunda cuenta y completar login desde Claude.
4. Confirmar un único proceso raíz/ventana de Claude.
5. Cambiar A → B → A desde el tray.
6. Confirmar identidad correcta, persistencia después de reiniciar y ausencia de
   procesos `claude-desktop-swap add/use` huérfanos.
7. Cancelar un alta y comprobar que la cuenta anterior reaparece automáticamente.

## Criterios de aceptación

- Después del nombre aparece exactamente una ventana de login de Claude.
- Claude no se cierra hasta que el usuario pulse **Finalizar registro** o
  **Cancelar**.
- Nunca hay más de un tray ni más de una operación activa.
- Todo error es visible y conserva/restaura una sesión usable.
- Un alta correcta termina con Claude abierto y el nuevo perfil marcado activo.
- Los switches A ↔ B funcionan tres veces consecutivas sin ventanas duplicadas.
- Claude Code no se detiene y ningún valor de cookie se imprime o registra.
