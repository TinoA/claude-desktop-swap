#define WIN32_LEAN_AND_MEAN
#define UNICODE
#define _UNICODE

#include <windows.h>
#include <shellapi.h>
#include <strsafe.h>

#define PRODUCT_NAME L"Windows Claude Swap"
#define TARGET_NAME L"claude-desktop-swap.exe"
#define PATH_CAPACITY 32768

static void show_launch_error(DWORD error)
{
    WCHAR system_message[512] = L"";
    WCHAR message[768] = L"";

    FormatMessageW(
        FORMAT_MESSAGE_FROM_SYSTEM | FORMAT_MESSAGE_IGNORE_INSERTS,
        NULL,
        error,
        0,
        system_message,
        ARRAYSIZE(system_message),
        NULL);
    if (system_message[0] == L'\0') {
        StringCchCopyW(system_message, ARRAYSIZE(system_message), L"Unknown Windows error.");
    }
    StringCchPrintfW(
        message,
        ARRAYSIZE(message),
        L"Windows Claude Swap could not start.\n\n%s\n\nError code: %lu",
        system_message,
        error);
    MessageBoxW(NULL, message, L"Could not start Windows Claude Swap", MB_OK | MB_ICONERROR | MB_SETFOREGROUND);
}

static BOOL sibling_paths(WCHAR *directory, size_t directory_capacity, WCHAR *target, size_t target_capacity)
{
    DWORD length = GetModuleFileNameW(NULL, directory, (DWORD)directory_capacity);
    WCHAR *separator;

    if (length == 0 || length >= directory_capacity - 1) {
        SetLastError(length == 0 ? GetLastError() : ERROR_INSUFFICIENT_BUFFER);
        return FALSE;
    }
    separator = directory + length;
    while (separator > directory && separator[-1] != L'\\' && separator[-1] != L'/') {
        --separator;
    }
    if (separator == directory) {
        SetLastError(ERROR_BAD_PATHNAME);
        return FALSE;
    }
    separator[-1] = L'\0';
    if (FAILED(StringCchCopyW(target, target_capacity, directory)) ||
        FAILED(StringCchCatW(target, target_capacity, L"\\" TARGET_NAME))) {
        SetLastError(ERROR_INSUFFICIENT_BUFFER);
        return FALSE;
    }
    return TRUE;
}

int WINAPI wWinMain(HINSTANCE instance, HINSTANCE previous, PWSTR command_line, int show_command)
{
    WCHAR directory[PATH_CAPACITY];
    WCHAR target[PATH_CAPACITY];
    SHELLEXECUTEINFOW launch = {0};
    DWORD attributes;

    UNREFERENCED_PARAMETER(instance);
    UNREFERENCED_PARAMETER(previous);
    UNREFERENCED_PARAMETER(command_line);
    UNREFERENCED_PARAMETER(show_command);

    SetErrorMode(SEM_FAILCRITICALERRORS | SEM_NOOPENFILEERRORBOX);
    if (!sibling_paths(directory, ARRAYSIZE(directory), target, ARRAYSIZE(target))) {
        show_launch_error(GetLastError());
        return 1;
    }
    attributes = GetFileAttributesW(target);
    if (attributes == INVALID_FILE_ATTRIBUTES || (attributes & FILE_ATTRIBUTE_DIRECTORY) != 0) {
        show_launch_error(attributes == INVALID_FILE_ATTRIBUTES ? GetLastError() : ERROR_FILE_NOT_FOUND);
        return 1;
    }

    launch.cbSize = sizeof(launch);
    launch.fMask = SEE_MASK_FLAG_NO_UI | SEE_MASK_NOASYNC;
    launch.lpVerb = L"open";
    launch.lpFile = target;
    launch.lpParameters = L"tray";
    launch.lpDirectory = directory;
    launch.nShow = SW_SHOWNORMAL;
    if (!ShellExecuteExW(&launch)) {
        show_launch_error(GetLastError());
        return 1;
    }
    return 0;
}
