# Fingerprint Media Converter API

High-performance media conversion microservice with **anti-fingerprinting** technology, designed to prevent WhatsApp detection. Built with **Go**, **Fiber v3**, and **FFmpeg** to achieve >1000 requests/second with per-device intelligent caching.

## 🎯 Key Features

- **Anti-Fingerprinting Technology**: Randomized parameters prevent file fingerprint detection
- **Per-Device Intelligent Cache**: 28-minute cache with 30-minute file expiration
- **Worker Pool + Buffer Pool**: Deterministic latency under burst loads
- **Audio/Image/Video Support**: Unified API for all media types
- **S3/Minio Integration**: Direct download from object storage
- **WhatsApp Optimized**: Formats and settings tested for WhatsApp compatibility

## 🏗️ Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                     Fingerprint Converter                    │
├─────────────────────────────────────────────────────────────┤
│  Fiber v3 HTTP Server (5001)                                │
├─────────────────────────────────────────────────────────────┤
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐     │
│  │ Audio Conv.  │  │ Image Conv.  │  │ Video Conv.  │     │
│  │ • Opus       │  │ • JPEG/PNG   │  │ • MP4/H.264  │     │
│  │ • Moderate   │  │ • Moderate   │  │ • Basic      │     │
│  └──────────────┘  └──────────────┘  └──────────────┘     │
├─────────────────────────────────────────────────────────────┤
│  ┌────────────────────────────────────────────────────┐    │
│  │  Device Cache (28/30 min TTL)                      │    │
│  │  • Per-device namespaces                           │    │
│  │  • Fixed expiration (no renewal)                   │    │
│  │  • Automatic cleanup                               │    │
│  └────────────────────────────────────────────────────┘    │
├─────────────────────────────────────────────────────────────┤
│  ┌──────────────┐         ┌──────────────┐                │
│  │ Worker Pool  │         │ Buffer Pool  │                │
│  │ 64 workers   │         │ 100x10MB     │                │
│  └──────────────┘         └──────────────┘                │
└─────────────────────────────────────────────────────────────┘
```

## 📦 Anti-Fingerprinting Levels

### Audio (Opus 48kHz Mono)
- **none**: No modifications
- **basic**: Bitrate 70-74k, compression 8-10, silence padding 1-3ms
- **moderate** ⭐: + pitch shift ±0.001
- **paranoid**: + noise, extended ranges

### Image (JPEG/PNG)
- **none**: No modifications  
- **basic**: Quality 88-92, minimal noise
- **moderate** ⭐: + color adjustment, format-specific noise (PNG lower)
- **paranoid**: + blur, extended ranges

### Video (MP4 H.264)
- **none**: No modifications
- **basic** ⭐: Relative bitrate ±5-10%, CRF 22-24, keyframe 240-260
- **moderate**: + noise, color adjustment, audio re-encode
- **paranoid**: + timestamp metadata, preset variation

⭐ = Recommended for WhatsApp use

## 🚀 Quick Start

### Using Docker (Recommended)

```bash
# Clone the repository
git clone <your-repo>
cd fingerprint-converter

# Build and run
docker-compose up -d

# Check logs
docker-compose logs -f

# Check health
curl http://localhost:5001/api/health
```

### Local Development

```bash
# Install Go 1.23+
# Install FFmpeg

# Run
go mod download
go run cmd/api/main.go
```

## 📡 API Endpoints

### POST /api/convert
Convert media with anti-fingerprinting.

**Request:**
```json
{
  "device_id": "device123",
  "url": "https://s3.example.com/file.mp3",
  "media_type": "audio",
  "anti_fingerprint_level": "moderate",
  "is_base64": false
}
```

**Response:**
```json
{
  "success": true,
  "processed_path": "/tmp/media-cache/device123_a1b2c3d4_1734567890.opus",
  "cache_hit": false,
  "media_type": "audio",
  "original_size_bytes": 245760,
  "processed_size_bytes": 281893,
  "size_increase_percent": "14.70%",
  "processing_time_ms": "1230",
  "cache_expires": "2025-12-18T15:28:00Z",
  "file_expires": "2025-12-18T15:30:00Z"
}
```

### GET /api/cache/stats/:deviceID
Get cache statistics for a specific device or globally.

**Response:**
```json
{
  "device_id": "device123",
  "device_stats": {
    "entries": 5,
    "total_kb": 1024,
    "cache_ttl": 28,
    "file_ttl": 30
  },
  "global_stats": {
    "devices": 10,
    "entries": 50,
    "total_mb": 50,
    "hits": 150,
    "misses": 45,
    "evictions": 12,
    "hit_rate": "76.92%"
  }
}
```

### GET /api/health
Health check with system metrics.

## 🔗 Integration Example (Node.js)

```javascript
const axios = require('axios');
const { exec } = require('child_process');
const util = require('util');
const execPromise = util.promisify(exec);

const CONVERTER_API = 'http://fingerprint-converter:5001';
const DEVICE_ID = 'device_001';

async function sendWhatsAppMedia(s3Url, recipientJid, mediaType) {
  try {
    // 1. Convert with anti-fingerprinting
    console.log(`🔄 Converting ${mediaType}...`);
    const response = await axios.post(`${CONVERTER_API}/api/convert`, {
      device_id: DEVICE_ID,
      url: s3Url,
      media_type: mediaType,
      anti_fingerprint_level: mediaType === 'video' ? 'basic' : 'moderate'
    });

    const { processed_path, cache_hit } = response.data;
    console.log(`✅ Converted! Cache hit: ${cache_hit}`);

    // 2. Push to Android device via ADB
    const devicePath = `/sdcard/Download/${Date.now()}.${getExtension(mediaType)}`;
    await execPromise(`adb -s ${DEVICE_ID} push "${processed_path}" "${devicePath}"`);

    // 3. Send via WhatsApp intent
    const mimeType = getMimeType(mediaType);
    await execPromise(`adb -s ${DEVICE_ID} shell am start \\
      -a android.intent.action.SEND \\
      -t "${mimeType}" \\
      --eu android.intent.extra.STREAM "file://${devicePath}" \\
      --es jid "${recipientJid}" \\
      -n com.whatsapp/.ContactPicker`);

    console.log('📤 Sent to WhatsApp!');

    // 4. Cleanup device file after send
    setTimeout(async () => {
      await execPromise(`adb -s ${DEVICE_ID} shell rm "${devicePath}"`);
    }, 5000);

  } catch (error) {
    console.error('❌ Error:', error.message);
    throw error;
  }
}

function getExtension(type) {
  const ext = { audio: 'opus', image: 'jpg', video: 'mp4' };
  return ext[type] || 'bin';
}

function getMimeType(type) {
  const mime = { 
    audio: 'audio/ogg', 
    image: 'image/jpeg', 
    video: 'video/mp4' 
  };
  return mime[type] || 'application/octet-stream';
}

// Usage
sendWhatsAppMedia(
  'https://s3.example.com/audio.mp3',
  '5511999999999@s.whatsapp.net',
  'audio'
);
```

## ⚙️ Configuration

See [.env.example](.env.example) for all configuration options.

**Key Settings:**
- `CACHE_TTL=28m` - Cache expires at 28 minutes
- `FILE_TTL=30m` - File deleted at 30 minutes (2-minute safety buffer)
- `MAX_WORKERS=64` - Worker pool size (0 = auto)
- `DEFAULT_AF_LEVEL=moderate` - Default anti-fingerprint level

## 📊 Performance

**Tested on 4-core 8GB server:**
- Audio conversion: ~1-2s per file
- Image conversion: ~0.5-1s per file
- Video conversion: ~5-15s per file (depends on duration)
- Throughput: >100 concurrent conversions
- Cache hit rate: 70-90% (typical usage)

**File Size Impact:**
- Audio: +10-20% (acceptable)
- Image: +40-60% (PNG with noise)
- Video: +30-50% (bitrate variation)

## 🛠️ Development

```bash
# Run locally
go run cmd/api/main.go

# Build
go build -o fingerprint-converter cmd/api/main.go

# Docker build
docker build -t fingerprint-converter .

# Run tests (TODO)
go test ./...
```

## 📝 License

MIT License - See LICENSE file

## 👤 Author

Built with ❤️ for WhatsApp automation

---

**Need help?** Open an issue or check the integration examples above.
