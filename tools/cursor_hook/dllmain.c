/*
 * cursor_hook.dll — hooks GetCursorPos in the target process via IAT patching.
 * Shared memory (file mapping) IPC with the Go controller.
 *
 * Build (MSVC / Visual Studio Developer Command Prompt):
 *   cl /LD /O2 /W4 dllmain.c user32.lib kernel32.lib advapi32.lib
 *
 * Build (MinGW-w64):
 *   x86_64-w64-mingw32-gcc -shared -O2 -Wall dllmain.c -o cursor_hook.dll -luser32 -lkernel32 -ladvapi32
 *
 * Or cross-compile from Linux:
 *   x86_64-w64-mingw32-gcc -shared -O2 dllmain.c -o cursor_hook.dll
 */

#define WIN32_LEAN_AND_MEAN
#include <windows.h>
#include <stdio.h>
#include <string.h>

#pragma comment(lib, "user32.lib")
#pragma comment(lib, "advapi32.lib")

/* ------------------------------------------------------------
   Shared memory IPC
   ------------------------------------------------------------ */
static HANDLE g_shmem = NULL;
static HANDLE g_mutex = NULL;
static int    g_inited = 0;

typedef struct _CURSOR_SHARED {
    volatile LONG  enabled;
    volatile LONG  x;
    volatile LONG  y;
    volatile LONG  pid;          /* PID of Go controller */
} CURSOR_SHARED;

static CURSOR_SHARED *g_shared = NULL;

#define SHMEM_NAME  L"KaTools_CursorHook"
#define MUTEX_NAME  L"KaTools_CursorHook_Mutex"

static int init_shmem(void) {
    if (g_inited) return 1;

    g_mutex = CreateMutexW(NULL, FALSE, MUTEX_NAME);
    if (!g_mutex) {
        OutputDebugStringW(L"[KaTools] cursor_hook: CreateMutex failed\n");
        return 0;
    }

    g_shmem = CreateFileMappingW(
        INVALID_HANDLE_VALUE, NULL,
        PAGE_READWRITE, 0, sizeof(CURSOR_SHARED), SHMEM_NAME);
    if (!g_shmem) {
        OutputDebugStringW(L"[KaTools] cursor_hook: CreateFileMapping failed\n");
        CloseHandle(g_mutex);
        return 0;
    }

    g_shared = (CURSOR_SHARED *)MapViewOfFile(
        g_shmem, FILE_MAP_ALL_ACCESS, 0, 0, sizeof(CURSOR_SHARED));
    if (!g_shared) {
        OutputDebugStringW(L"[KaTools] cursor_hook: MapViewOfFile failed\n");
        CloseHandle(g_shmem);
        CloseHandle(g_mutex);
        return 0;
    }

    g_shared->enabled = 0;
    g_shared->x = 0;
    g_shared->y = 0;
    g_shared->pid = 0;
    g_inited = 1;

    OutputDebugStringW(L"[KaTools] cursor_hook: shared memory initialized\n");
    return 1;
}

static void get_fake_pos(LPPOINT pt) {
    if (!g_shared || !g_shared->enabled || !g_shared->pid)
        return;

    /* Verify the controller process is still alive */
    DWORD pid = (DWORD)g_shared->pid;
    HANDLE h = OpenProcess(SYNCHRONIZE, FALSE, pid);
    if (h) {
        DWORD ret = WaitForSingleObject(h, 50);
        CloseHandle(h);
        if (ret != WAIT_OBJECT_0)
            return;  /* controller dead, don't fake */
    } else {
        return;  /* can't open process */
    }

    pt->x = (LONG)g_shared->x;
    pt->y = (LONG)g_shared->y;
}

/* ------------------------------------------------------------
   IAT (Import Address Table) patching
   Finds user32.dll!GetCursorPos in the current process's IAT
   and patches it to jump to our hook.
   ------------------------------------------------------------ */
static int is_code_addr(LPCVOID addr) {
    MEMORY_BASIC_INFORMATION mbi = {0};
    if (!VirtualQuery(addr, &mbi, sizeof(mbi)))
        return 0;
    return (mbi.State == MEM_COMMIT) &&
           ((mbi.Protect & (PAGE_EXECUTE_READ |
                            PAGE_EXECUTE_READWRITE |
                            PAGE_EXECUTE_WRITECOPY)) != 0);
}

/* Patch [addr] with a JMP [rip+imm32] to target */
static int patch_jmp_abs(LPVOID addr, LPCVOID target) {
    BYTE code[14];
    code[0] = 0xFF;  /* JMP [disp32] */
    code[1] = 0x25;
    *(DWORD *)(code + 2) = 0;  /* disp32 = 0 (next instruction) */
    *(LPVOID *)(code + 6) = (LPVOID)target;  /* absolute ptr */

    DWORD old;
    if (!VirtualProtect(addr, sizeof(code), PAGE_READWRITE, &old))
        return 0;
    memcpy(addr, code, sizeof(code));
    VirtualProtect(addr, sizeof(code), old, &old);
    FlushInstructionCache(GetCurrentProcess(), addr, sizeof(code));
    return 1;
}

static void *find_iat_entry(LPCVOID module, PCSTR proc_name) {
    IMAGE_DOS_HEADER       *dos = (IMAGE_DOS_HEADER *)module;
    IMAGE_NT_HEADERS       *nt;
    IMAGE_IMPORT_DESCRIPTOR *imp;

    if (dos->e_magic != IMAGE_DOS_SIGNATURE)
        return NULL;

    nt = (IMAGE_NT_HEADERS *)((PBYTE)module + dos->e_lfanew);
    if (nt->Signature != IMAGE_NT_SIGNATURE)
        return NULL;

    if (nt->OptionalHeader.NumberOfRvaAndSizes <=
        IMAGE_DIRECTORY_ENTRY_IMPORT)
        return NULL;

    imp = (IMAGE_IMPORT_DESCRIPTOR *)
          ((PBYTE)module +
           nt->OptionalHeader.DataDirectory[IMAGE_DIRECTORY_ENTRY_IMPORT].VirtualAddress);

    for (; imp->Name; imp++) {
        PCSTR modname = (PCSTR)((PBYTE)module + imp->Name);
        if (lstrcmpiA(modname, "user32.dll") != 0 &&
            lstrcmpiA(modname, "USER32.DLL") != 0)
            continue;

        IMAGE_THUNK_DATA *thunk = (IMAGE_THUNK_DATA *)
            ((PBYTE)module + imp->OriginalFirstThunk);
        for (; thunk->u1.Function; thunk++) {
            IMAGE_IMPORT_BY_NAME *name = (IMAGE_IMPORT_BY_NAME *)
                ((PBYTE)module + thunk->u1.AddressOfData);
            if (name->Hint != 0)
                continue;
            if (strcmp(name->Name, proc_name) == 0)
                return (PBYTE)&thunk->u1.Function;
        }
    }
    return NULL;
}

/* Original function pointer */
static int (WINAPI *orig_GetCursorPos)(LPPOINT) = NULL;

/* Hook: call real GetCursorPos, then override with fake if enabled */
static int WINAPI hook_GetCursorPos(LPPOINT pt) {
    int ret = orig_GetCursorPos(pt);
    get_fake_pos(pt);
    return ret;
}

/* Try to patch user32.dll!GetCursorPos in the IAT */
static int patch_user32_GetCursorPos(void) {
    void *user32 = (void *)GetModuleHandleW(L"user32.dll");
    if (!user32) {
        OutputDebugStringW(L"[KaTools] cursor_hook: user32.dll not found\n");
        return 0;
    }

    /* Find our own IAT entry for GetCursorPos */
    void **entry = (void **)find_iat_entry(user32, "GetCursorPos");
    if (!entry) {
        OutputDebugStringW(L"[KaTools] cursor_hook: IAT entry not found\n");
        return 0;
    }

    /* Save original address */
    orig_GetCursorPos = (void *)*entry;
    if ((LPVOID)orig_GetCursorPos == (LPVOID)hook_GetCursorPos) {
        OutputDebugStringW(L"[KaTools] cursor_hook: already hooked\n");
        return 1;  /* already patched */
    }

    if (!is_code_addr(*entry)) {
        OutputDebugStringW(L"[KaTools] cursor_hook: target is not in code memory\n");
        return 0;
    }

    /* Patch IAT entry with JMP to our hook */
    if (!patch_jmp_abs(entry, hook_GetCursorPos)) {
        OutputDebugStringW(L"[KaTools] cursor_hook: patch_jmp_abs failed\n");
        return 0;
    }

    wchar_t dbg[128];
    wsprintfW(dbg, L"[KaTools] cursor_hook: patched IAT %p -> %p (was %p)\n",
              entry, hook_GetCursorPos, orig_GetCursorPos);
    OutputDebugStringW(dbg);
    return 1;
}

/* ------------------------------------------------------------
   DLL entry point
   ------------------------------------------------------------ */
static int g_hook_ready = 0;

BOOL WINAPI DllMain(HINSTANCE hinst, DWORD reason, LPVOID reserved) {
    (void)hinst;
    (void)reserved;

    if (reason == DLL_PROCESS_ATTACH) {
        DisableThreadLibraryCalls(hinst);
        init_shmem();
        g_hook_ready = patch_user32_GetCursorPos();
    }

    if (reason == DLL_PROCESS_DETACH) {
        if (g_shared) {
            UnmapViewOfFile(g_shared);
            g_shared = NULL;
        }
        if (g_shmem) {
            CloseHandle(g_shmem);
            g_shmem = NULL;
        }
        if (g_mutex) {
            CloseHandle(g_mutex);
            g_mutex = NULL;
        }
        g_inited = 0;
    }

    return TRUE;
}

/* ------------------------------------------------------------
   Exported functions (called from Go via cgo or LoadLibrary + GetProcAddress)
   ------------------------------------------------------------ */

/* Enable/disable fake cursor position */
__declspec(dllexport) void __cdecl SetFakeCursorPos(int x, int y, int pid, int enabled) {
    if (!g_shared) return;
    WaitForSingleObject(g_mutex, INFINITE);
    g_shared->x = x;
    g_shared->y = y;
    g_shared->pid = pid;
    g_shared->enabled = enabled ? 1 : 0;
    ReleaseMutex(g_mutex);
}

/* Check if hook is active */
__declspec(dllexport) int __cdecl IsHookReady(void) {
    return g_hook_ready;
}
