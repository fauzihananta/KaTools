#include <windows.h>
#include <roapi.h>
#include <wrl.h>

#include <d3d11.h>
#include <dxgi1_2.h>

#include <windows.graphics.capture.h>
#include <windows.graphics.capture.interop.h>
#include <windows.graphics.directx.h>
#include <windows.graphics.directx.direct3d11.interop.h>

#include <iostream>
#include <cstdint>
#include <cstring>
#include <string>

#pragma comment(lib, "d3d11.lib")
#pragma comment(lib, "windowsapp.lib")
#pragma comment(lib, "user32.lib")

using Microsoft::WRL::ComPtr;
using Microsoft::WRL::Wrappers::HStringReference;


// ============================================================
// Shared Memory
// ============================================================

static const wchar_t* SHARED_MEMORY_NAME =
    L"Local\\KaToolsKathanaFrameV2";

static const uint32_t SHARED_MEMORY_MAGIC =
    0x4B544652; // "KTFR"

// KaTools only needs fresh frames for UI detection, not video-quality capture.
// Keeping the capture at 15 FPS substantially reduces GPU->CPU copies and
// shared-memory writes on battery while retaining responsive popup detection.
static const DWORD CAPTURE_FPS = 15;
static const DWORD CAPTURE_FRAME_INTERVAL_MS = 1000 / CAPTURE_FPS;


#pragma pack(push, 1)

struct SharedFrameHeader
{
    uint32_t magic;

    uint32_t width;
    uint32_t height;

    uint32_t stride;
    uint32_t bytesPerPixel;

    uint64_t frameNumber;

    uint32_t dataSize;

    // 0 = writing
    // 1 = frame ready
    volatile LONG frameReady;
};

#pragma pack(pop)


// ============================================================
// IDirect3DDxgiInterfaceAccess
// ============================================================

struct __declspec(uuid(
    "A9B3D012-3DF2-4EE3-B8D1-8695F457D3C1"
))
IDirect3DDxgiInterfaceAccess : public IUnknown
{
    virtual HRESULT STDMETHODCALLTYPE GetInterface(
        REFIID iid,
        void** p
    ) = 0;
};


// ============================================================
// Shared Memory Context
// ============================================================

struct SharedMemoryContext
{
    HANDLE mapping;

    void* memory;

    size_t totalSize;

    SharedFrameHeader* header;

    uint8_t* pixels;
};


bool CreateSharedMemory(
    SharedMemoryContext& shm,
    uint32_t width,
    uint32_t height
)
{
    memset(
        &shm,
        0,
        sizeof(shm)
    );

    const uint32_t bytesPerPixel = 4;

    uint32_t stride =
        width * bytesPerPixel;

    size_t pixelBytes =
        static_cast<size_t>(stride) *
        static_cast<size_t>(height);

    size_t totalSize =
        sizeof(SharedFrameHeader) +
        pixelBytes;

    std::cout
        << "Creating shared memory..."
        << std::endl;

    std::cout
        << "Width: "
        << width
        << std::endl;

    std::cout
        << "Height: "
        << height
        << std::endl;

    std::cout
        << "Stride: "
        << stride
        << std::endl;

    std::cout
        << "Pixel bytes: "
        << pixelBytes
        << std::endl;

    std::cout
        << "Shared memory size: "
        << totalSize
        << " bytes"
        << std::endl;

    // --------------------------------------------------------
    // Create file mapping
    // --------------------------------------------------------

    DWORD sizeHigh =
        static_cast<DWORD>(
            (static_cast<uint64_t>(totalSize) >> 32)
        );

    DWORD sizeLow =
        static_cast<DWORD>(
            static_cast<uint64_t>(totalSize) &
            0xFFFFFFFFULL
        );

   shm.mapping =
    CreateFileMappingW(
        INVALID_HANDLE_VALUE,
        nullptr,
        PAGE_READWRITE,
        sizeHigh,
        sizeLow,
        SHARED_MEMORY_NAME
    );

if (!shm.mapping)
{
    std::cout
        << "CreateFileMapping failed: "
        << GetLastError()
        << std::endl;

    return false;
}

DWORD createError = GetLastError();

if (createError == ERROR_ALREADY_EXISTS)
{
    std::cout
        << "WARNING: shared memory already exists!"
        << std::endl;
}
else
{
    std::cout
        << "Shared memory created successfully...!"
        << std::endl;
}

    // --------------------------------------------------------
    // Map memory
    // --------------------------------------------------------

    shm.memory =
        MapViewOfFile(
            shm.mapping,
            FILE_MAP_READ | FILE_MAP_WRITE,
            0,
            0,
            totalSize
        );

    if (!shm.memory)
    {
        std::cout
            << "MapViewOfFile failed: "
            << GetLastError()
            << std::endl;

        CloseHandle(shm.mapping);

        shm.mapping = nullptr;

        return false;
    }

    shm.totalSize =
        totalSize;

    shm.header =
        reinterpret_cast<SharedFrameHeader*>(
            shm.memory
        );

    shm.pixels =
        reinterpret_cast<uint8_t*>(
            shm.memory
        ) +
        sizeof(SharedFrameHeader);

    // --------------------------------------------------------
    // Initialize header
    // --------------------------------------------------------

    memset(
        shm.memory,
        0,
        totalSize
    );

    shm.header->magic =
        SHARED_MEMORY_MAGIC;

    shm.header->width =
        width;

    shm.header->height =
        height;

    shm.header->stride =
        stride;

    shm.header->bytesPerPixel =
        bytesPerPixel;

    shm.header->frameNumber =
        0;

    shm.header->dataSize =
        static_cast<uint32_t>(
            pixelBytes
        );

    InterlockedExchange(
        &shm.header->frameReady,
        0
    );

    std::cout
        << "Shared memory OK"
        << std::endl;

    std::cout
        << "Name: KaToolsKathanaFrame"
        << std::endl;

    return true;
}


// ============================================================
// Destroy Shared Memory
// ============================================================

void DestroySharedMemory(
    SharedMemoryContext& shm
)
{
    if (shm.memory)
    {
        UnmapViewOfFile(
            shm.memory
        );

        shm.memory = nullptr;
    }

    if (shm.mapping)
    {
        CloseHandle(
            shm.mapping
        );

        shm.mapping = nullptr;
    }

    shm.header = nullptr;
    shm.pixels = nullptr;
}


// ============================================================
// Write Frame To Shared Memory
// ============================================================

bool WriteFrameToSharedMemory(
    SharedMemoryContext& shm,
    const D3D11_MAPPED_SUBRESOURCE& mapped,
    uint32_t width,
    uint32_t height
)
{
    if (!shm.header ||
        !shm.pixels)
    {
        return false;
    }

    const uint32_t bytesPerPixel = 4;

    const uint32_t outputStride =
        width * bytesPerPixel;

    const size_t outputSize =
        static_cast<size_t>(
            outputStride
        ) *
        static_cast<size_t>(
            height
        );

    // --------------------------------------------------------
    // Mark frame as being written
    // --------------------------------------------------------

    InterlockedExchange(
        &shm.header->frameReady,
        0
    );

    // --------------------------------------------------------
    // Copy row by row
    //
    // mapped.RowPitch may be different from
    // width * 4, so DON'T memcpy the entire
    // frame blindly.
    // --------------------------------------------------------

    const uint8_t* source =
        reinterpret_cast<const uint8_t*>(
            mapped.pData
        );

    uint8_t* destination =
        shm.pixels;

    for (uint32_t y = 0; y < height; y++)
    {
        memcpy(
            destination +
                static_cast<size_t>(y) *
                outputStride,

            source +
                static_cast<size_t>(y) *
                mapped.RowPitch,

            outputStride
        );
    }

    // --------------------------------------------------------
    // Update metadata
    // --------------------------------------------------------

    shm.header->width =
        width;

    shm.header->height =
        height;

    shm.header->stride =
        outputStride;

    shm.header->bytesPerPixel =
        bytesPerPixel;

    shm.header->dataSize =
        static_cast<uint32_t>(
            outputSize
        );

    shm.header->frameNumber++;

    // --------------------------------------------------------
    // Frame ready
    // --------------------------------------------------------

    InterlockedExchange(
        &shm.header->frameReady,
        1
    );

    return true;
}


// ============================================================
// Capture Kathana
// ============================================================

bool CaptureWindow(
    HWND hwnd,
    ID3D11Device* device,
    ID3D11DeviceContext* context
)
{
    std::cout
        << std::endl
        << "------------------------------------"
        << std::endl;

    std::cout
        << "Connecting to selected window..."
        << std::endl;

    // --------------------------------------------------------
    // DXGI device
    // --------------------------------------------------------

    ComPtr<IDXGIDevice> dxgiDevice;

    HRESULT hr =
        device->QueryInterface(
            IID_PPV_ARGS(
                &dxgiDevice
            )
        );

    if (FAILED(hr))
    {
        std::cout
            << "IDXGIDevice failed."
            << std::endl;

        return false;
    }

    // --------------------------------------------------------
    // WinRT D3D11 device
    // --------------------------------------------------------

    ComPtr<IInspectable>
        inspectableDevice;

    hr =
        CreateDirect3D11DeviceFromDXGIDevice(
            dxgiDevice.Get(),
            &inspectableDevice
        );

    if (FAILED(hr))
    {
        std::cout
            << "CreateDirect3D11DeviceFromDXGIDevice failed: 0x"
            << std::hex
            << hr
            << std::dec
            << std::endl;

        return false;
    }

    ComPtr<
        ABI::Windows::Graphics::DirectX::Direct3D11::
        IDirect3DDevice
    > direct3DDevice;

    hr =
        inspectableDevice.As(
            &direct3DDevice
        );

    if (FAILED(hr))
    {
        std::cout
            << "IDirect3DDevice query failed."
            << std::endl;

        return false;
    }

    // --------------------------------------------------------
    // GraphicsCaptureItem factory
    // --------------------------------------------------------

    ComPtr<
        IGraphicsCaptureItemInterop
    > itemInterop;

    hr =
        RoGetActivationFactory(
            HStringReference(
                RuntimeClass_Windows_Graphics_Capture_GraphicsCaptureItem
            ).Get(),
            IID_PPV_ARGS(
                &itemInterop
            )
        );

    if (FAILED(hr))
    {
        std::cout
            << "GraphicsCaptureItem factory failed."
            << std::endl;

        return false;
    }

    // --------------------------------------------------------
    // Create GraphicsCaptureItem
    // --------------------------------------------------------

    ComPtr<
        ABI::Windows::Graphics::Capture::
        IGraphicsCaptureItem
    > item;

    hr =
        itemInterop->CreateForWindow(
            hwnd,
            IID_PPV_ARGS(
                &item
            )
        );

    if (FAILED(hr))
    {
        std::cout
            << "CreateForWindow failed: 0x"
            << std::hex
            << hr
            << std::dec
            << std::endl;

        return false;
    }

    std::cout
        << "GraphicsCaptureItem OK"
        << std::endl;

    // --------------------------------------------------------
    // Get size
    // --------------------------------------------------------

    ABI::Windows::Graphics::SizeInt32 size;

    hr =
        item->get_Size(
            &size
        );

    if (FAILED(hr))
    {
        std::cout
            << "get_Size failed."
            << std::endl;

        return false;
    }

    std::cout
        << "Capture size: "
        << size.Width
        << "x"
        << size.Height
        << std::endl;

    // --------------------------------------------------------
    // FramePool factory
    // --------------------------------------------------------

    ComPtr<
        ABI::Windows::Graphics::Capture::
        IDirect3D11CaptureFramePoolStatics
    > framePoolStatics;

    hr =
        RoGetActivationFactory(
            HStringReference(
                RuntimeClass_Windows_Graphics_Capture_Direct3D11CaptureFramePool
            ).Get(),
            IID_PPV_ARGS(
                &framePoolStatics
            )
        );

    if (FAILED(hr))
    {
        std::cout
            << "FramePool factory failed."
            << std::endl;

        return false;
    }

    // --------------------------------------------------------
    // Create FramePool
    // --------------------------------------------------------

    ComPtr<
        ABI::Windows::Graphics::Capture::
        IDirect3D11CaptureFramePool
    > framePool;

    hr =
        framePoolStatics->Create(
            direct3DDevice.Get(),

            ABI::Windows::Graphics::DirectX::
            DirectXPixelFormat_B8G8R8A8UIntNormalized,

            2,

            size,

            &framePool
        );

    if (FAILED(hr))
    {
        std::cout
            << "FramePool Create failed: 0x"
            << std::hex
            << hr
            << std::dec
            << std::endl;

        return false;
    }

    std::cout
        << "FramePool OK"
        << std::endl;

    // --------------------------------------------------------
    // Create capture session
    // --------------------------------------------------------

    ComPtr<
        ABI::Windows::Graphics::Capture::
        IGraphicsCaptureSession
    > session;

    hr =
        framePool->CreateCaptureSession(
            item.Get(),
            &session
        );

    if (FAILED(hr))
    {
        std::cout
            << "CreateCaptureSession failed."
            << std::endl;

        return false;
    }

    // --------------------------------------------------------
    // Start capture
    // --------------------------------------------------------

    hr =
        session->StartCapture();

    if (FAILED(hr))
    {
        std::cout
            << "StartCapture failed: 0x"
            << std::hex
            << hr
            << std::dec
            << std::endl;

        return false;
    }

    std::cout
        << "Capture started."
        << std::endl;

    // --------------------------------------------------------
    // Shared memory
    // --------------------------------------------------------

    SharedMemoryContext shm;

    if (!CreateSharedMemory(
        shm,
        static_cast<uint32_t>(
            size.Width
        ),
        static_cast<uint32_t>(
            size.Height
        )
    ))
    {
        return false;
    }

    std::cout
        << "Ready for Go reader."
        << std::endl;

    std::cout
        << "Shared memory:"
        << std::endl;

    std::cout
        << "  KaToolsKathanaFrame"
        << std::endl;

    std::cout
        << "  Format: BGRA"
        << std::endl;

    std::cout
        << "  Resolution: "
        << size.Width
        << "x"
        << size.Height
        << std::endl;

    std::cout
        << std::endl;

    // --------------------------------------------------------
    // Continuous capture
    // --------------------------------------------------------

    uint64_t localFrameCount = 0;

    while (true)
    {
        const ULONGLONG frameStartedAt = GetTickCount64();

        // ----------------------------------------------------
        // Check Kathana still exists
        // ----------------------------------------------------

        if (!IsWindow(hwnd))
        {
            std::cout
                << std::endl
                << "Kathana window closed."
                << std::endl;

            DestroySharedMemory(
                shm
            );

            return false;
        }

        // ----------------------------------------------------
        // Get next frame
        // ----------------------------------------------------

        ComPtr<
            ABI::Windows::Graphics::Capture::
            IDirect3D11CaptureFrame
        > frame;

        hr =
            framePool->TryGetNextFrame(
                &frame
            );

        if (FAILED(hr) ||
            !frame)
        {
            Sleep(2);

            continue;
        }

        // ----------------------------------------------------
        // Surface
        // ----------------------------------------------------

        ComPtr<
            ABI::Windows::Graphics::DirectX::Direct3D11::
            IDirect3DSurface
        > surface;

        hr =
            frame->get_Surface(
                &surface
            );

        if (FAILED(hr))
        {
            std::cout
                << "get_Surface failed."
                << std::endl;

            DestroySharedMemory(
                shm
            );

            return false;
        }

        // ----------------------------------------------------
        // DXGI access
        // ----------------------------------------------------

        ComPtr<
            IDirect3DDxgiInterfaceAccess
        > access;

        hr =
            surface.As(
                &access
            );

        if (FAILED(hr))
        {
            std::cout
                << "DXGI interface access failed: 0x"
                << std::hex
                << hr
                << std::dec
                << std::endl;

            DestroySharedMemory(
                shm
            );

            return false;
        }

        // ----------------------------------------------------
        // Get D3D11 texture
        // ----------------------------------------------------

        ComPtr<ID3D11Texture2D>
            sourceTexture;

        hr =
            access->GetInterface(
                IID_PPV_ARGS(
                    &sourceTexture
                )
            );

        if (FAILED(hr))
        {
            std::cout
                << "GetInterface texture failed."
                << std::endl;

            DestroySharedMemory(
                shm
            );

            return false;
        }

        // ----------------------------------------------------
        // Texture description
        // ----------------------------------------------------

        D3D11_TEXTURE2D_DESC desc;

        sourceTexture->GetDesc(
            &desc
        );

        // ----------------------------------------------------
        // Create staging texture
        // ----------------------------------------------------

        D3D11_TEXTURE2D_DESC stagingDesc =
            desc;

        stagingDesc.Usage =
            D3D11_USAGE_STAGING;

        stagingDesc.BindFlags =
            0;

        stagingDesc.CPUAccessFlags =
            D3D11_CPU_ACCESS_READ;

        stagingDesc.MiscFlags =
            0;

        ComPtr<ID3D11Texture2D>
            stagingTexture;

        hr =
            device->CreateTexture2D(
                &stagingDesc,
                nullptr,
                &stagingTexture
            );

        if (FAILED(hr))
        {
            std::cout
                << "Create staging texture failed."
                << std::endl;

            DestroySharedMemory(
                shm
            );

            return false;
        }

        // ----------------------------------------------------
        // GPU -> staging
        // ----------------------------------------------------

        context->CopyResource(
            stagingTexture.Get(),
            sourceTexture.Get()
        );

        // ----------------------------------------------------
        // Map
        // ----------------------------------------------------

        D3D11_MAPPED_SUBRESOURCE mapped;

        hr =
            context->Map(
                stagingTexture.Get(),
                0,
                D3D11_MAP_READ,
                0,
                &mapped
            );

        if (FAILED(hr))
        {
            std::cout
                << "Map failed."
                << std::endl;

            DestroySharedMemory(
                shm
            );

            return false;
        }

        // ----------------------------------------------------
        // Write pixels to shared memory
        // ----------------------------------------------------

        bool success =
            WriteFrameToSharedMemory(
                shm,
                mapped,
                desc.Width,
                desc.Height
            );

        context->Unmap(
            stagingTexture.Get(),
            0
        );

        if (!success)
        {
            std::cout
                << "Failed to write frame."
                << std::endl;

            DestroySharedMemory(
                shm
            );

            return false;
        }

        localFrameCount++;

        // ----------------------------------------------------
        // Console status every 60 frames
        // ----------------------------------------------------

        if (
            localFrameCount % 60 == 0
        )
        {
            std::cout
                << "Shared frame: "
                << localFrameCount
                << " | "
                << desc.Width
                << "x"
                << desc.Height
                << std::endl;
        }

        // ----------------------------------------------------
        // Limit the expensive GPU->CPU copy and shared-memory write to 15 FPS.
        // Account for the work already spent on this frame so the cap stays
        // close to 15 FPS instead of being 15 FPS plus processing time.
        // ----------------------------------------------------

        const ULONGLONG elapsedMs =
            GetTickCount64() - frameStartedAt;

        if (elapsedMs < CAPTURE_FRAME_INTERVAL_MS)
        {
            Sleep(
                CAPTURE_FRAME_INTERVAL_MS -
                static_cast<DWORD>(elapsedMs)
            );
        }
    }

    DestroySharedMemory(
        shm
    );

    return false;
}


// ============================================================
// MAIN
// ============================================================

bool ParseHWND(
    int argc,
    char* argv[],
    HWND& hwnd
)
{
    hwnd = nullptr;

    for (int i = 1; i < argc; ++i)
    {
        if (
            std::string(argv[i]) == "--hwnd" &&
            i + 1 < argc
        )
        {
            const char* value = argv[++i];

            try
            {
                unsigned long long raw =
                    std::stoull(
                        value,
                        nullptr,
                        0
                    );

                hwnd =
                    reinterpret_cast<HWND>(
                        static_cast<uintptr_t>(raw)
                    );

                return hwnd != nullptr;
            }
            catch (...)
            {
                return false;
            }
        }
    }

    return false;
}


int main(
    int argc,
    char* argv[]
)
{
    std::cout
        << "========================================"
        << std::endl;

    std::cout
        << "     KaTools Shared Memory"
        << std::endl;

    std::cout
        << "========================================"
        << std::endl;

    std::cout
        << std::endl;

    // --------------------------------------------------------
    // Parse target HWND
    // --------------------------------------------------------

    HWND hwnd = nullptr;

    if (!ParseHWND(
        argc,
        argv,
        hwnd
    ))
    {
        std::cout
            << "Usage:"
            << std::endl;

        std::cout
            << "  wgc_capture.exe --hwnd 0x123456"
            << std::endl;

        return 1;
    }

    if (!IsWindow(hwnd))
    {
        std::cout
            << "Invalid HWND: 0x"
            << std::hex
            << reinterpret_cast<uintptr_t>(hwnd)
            << std::dec
            << std::endl;

        return 1;
    }

    wchar_t title[512] = {};

    GetWindowTextW(
        hwnd,
        title,
        static_cast<int>(
            sizeof(title) /
            sizeof(title[0])
        )
    );

    std::wcout
        << L"Target window : "
        << title
        << std::endl;

    std::cout
        << "HWND          : 0x"
        << std::hex
        << reinterpret_cast<uintptr_t>(hwnd)
        << std::dec
        << std::endl;

    // --------------------------------------------------------
    // WinRT
    // --------------------------------------------------------

    HRESULT hr =
        RoInitialize(
            RO_INIT_MULTITHREADED
        );

    if (FAILED(hr))
    {
        std::cout
            << "RoInitialize failed: 0x"
            << std::hex
            << hr
            << std::dec
            << std::endl;

        return 1;
    }

    // --------------------------------------------------------
    // COM
    // --------------------------------------------------------

    hr =
        CoInitializeEx(
            nullptr,
            COINIT_MULTITHREADED
        );

    if (FAILED(hr) &&
        hr != RPC_E_CHANGED_MODE)
    {
        std::cout
            << "CoInitializeEx failed: 0x"
            << std::hex
            << hr
            << std::dec
            << std::endl;

        RoUninitialize();

        return 1;
    }

    // --------------------------------------------------------
    // D3D11 device
    // --------------------------------------------------------

    ComPtr<ID3D11Device>
        device;

    ComPtr<ID3D11DeviceContext>
        context;

    D3D_FEATURE_LEVEL featureLevel;

    hr =
        D3D11CreateDevice(
            nullptr,
            D3D_DRIVER_TYPE_HARDWARE,
            nullptr,
            D3D11_CREATE_DEVICE_BGRA_SUPPORT,
            nullptr,
            0,
            D3D11_SDK_VERSION,
            &device,
            &featureLevel,
            &context
        );

    if (FAILED(hr))
    {
        std::cout
            << "D3D11CreateDevice failed: 0x"
            << std::hex
            << hr
            << std::dec
            << std::endl;

        CoUninitialize();
        RoUninitialize();

        return 1;
    }

    std::cout
        << "D3D11 device OK"
        << std::endl;

    // --------------------------------------------------------
    // Capture selected window
    // --------------------------------------------------------

    bool result =
        CaptureWindow(
            hwnd,
            device.Get(),
            context.Get()
        );

    if (!result)
    {
        std::cout
            << std::endl
            << "Capture session ended."
            << std::endl;
    }

    CoUninitialize();
    RoUninitialize();

    return result ? 0 : 1;
}
