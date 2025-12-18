/**
 * Fingerprint Converter - Node.js Integration Example
 * 
 * Complete example showing how to integrate the Fingerprint Converter API
 * with your WhatsApp automation system using Node.js + ADB.
 */

const axios = require('axios');
const { exec } = require('child_process');
const util = require('util');
const fs = require('fs').promises;
const path = require('path');

const execPromise = util.promisify(exec);

// Configuration
const CONFIG = {
  converterAPI: process.env.CONVERTER_API || 'http://localhost:5001',
  deviceID: process.env.DEVICE_ID || 'device_001',
  adbDevice: process.env.ADB_DEVICE || '192.168.1.100:5555',
  whatsappPackage: 'com.whatsapp',
  tempDir: '/sdcard/Download',
};

class FingerprintConverter {
  constructor(config) {
    this.config = config;
    this.client = axios.create({
      baseURL: config.converterAPI,
      timeout: 300000, // 5 minutes
    });
  }

  /**
   * Convert media with anti-fingerprinting
   */
  async convertMedia(s3Url, mediaType, level = null) {
    const defaultLevels = {
      audio: 'moderate',
      image: 'moderate',
      video: 'basic',
    };

    const payload = {
      device_id: this.config.deviceID,
      url: s3Url,
      media_type: mediaType,
      anti_fingerprint_level: level || defaultLevels[mediaType],
      is_base64: false,
    };

    console.log(`🔄 Converting ${mediaType}: ${this.truncateURL(s3Url)}`);
    const start = Date.now();

    try {
      const response = await this.client.post('/api/convert', payload);
      const duration = Date.now() - start;

      console.log(`✅ Converted in ${duration}ms (cache: ${response.data.cache_hit})`);
      console.log(`   Original: ${this.formatBytes(response.data.original_size_bytes)}`);
      console.log(`   Processed: ${this.formatBytes(response.data.processed_size_bytes)} (${response.data.size_increase_percent})`);

      return response.data;
    } catch (error) {
      console.error(`❌ Conversion failed: ${error.message}`);
      throw error;
    }
  }

  /**
   * Get cache statistics
   */
  async getCacheStats(deviceID = null) {
    const endpoint = deviceID 
      ? `/api/cache/stats/${deviceID}`
      : '/api/cache/stats';

    try {
      const response = await this.client.get(endpoint);
      return response.data;
    } catch (error) {
      console.error(`❌ Failed to get stats: ${error.message}`);
      throw error;
    }
  }

  /**
   * Check API health
   */
  async checkHealth() {
    try {
      const response = await this.client.get('/api/health');
      console.log('✅ API Health:', response.data.status);
      return response.data;
    } catch (error) {
      console.error(`❌ Health check failed: ${error.message}`);
      throw error;
    }
  }

  // Helper methods
  truncateURL(url) {
    return url.length > 60 ? url.substring(0, 57) + '...' : url;
  }

  formatBytes(bytes) {
    if (bytes < 1024) return bytes + ' B';
    if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(2) + ' KB';
    return (bytes / (1024 * 1024)).toFixed(2) + ' MB';
  }
}

class WhatsAppSender {
  constructor(config) {
    this.config = config;
  }

  /**
   * Push file to Android device via ADB
   */
  async pushToDevice(localPath, remotePath) {
    const cmd = `adb -s ${this.config.adbDevice} push "${localPath}" "${remotePath}"`;
    console.log(`📲 Pushing to device: ${path.basename(remotePath)}`);

    try {
      await execPromise(cmd);
      console.log(`✅ File pushed successfully`);
      return remotePath;
    } catch (error) {
      console.error(`❌ ADB push failed: ${error.message}`);
      throw error;
    }
  }

  /**
   * Send media via WhatsApp intent
   */
  async sendViaIntent(devicePath, recipientJid, mediaType) {
    const mimeType = this.getMimeType(mediaType);
    
    // Build WhatsApp intent command
    const cmd = `adb -s ${this.config.adbDevice} shell am start \\
      -a android.intent.action.SEND \\
      -t "${mimeType}" \\
      --eu android.intent.extra.STREAM "file://${devicePath}" \\
      --es jid "${recipientJid}" \\
      -n ${this.config.whatsappPackage}/.ContactPicker`;

    console.log(`📤 Sending to WhatsApp: ${recipientJid}`);

    try {
      await execPromise(cmd);
      console.log(`✅ Sent to WhatsApp successfully`);
    } catch (error) {
      console.error(`❌ WhatsApp send failed: ${error.message}`);
      throw error;
    }
  }

  /**
   * Cleanup file from device
   */
  async cleanupDevice(devicePath) {
    const cmd = `adb -s ${this.config.adbDevice} shell rm "${devicePath}"`;
    
    try {
      await execPromise(cmd);
      console.log(`🗑️  Cleaned up: ${path.basename(devicePath)}`);
    } catch (error) {
      console.warn(`⚠️  Cleanup warning: ${error.message}`);
    }
  }

  getMimeType(mediaType) {
    const mimeTypes = {
      audio: 'audio/ogg',
      image: 'image/jpeg',
      video: 'video/mp4',
    };
    return mimeTypes[mediaType] || 'application/octet-stream';
  }

  getExtension(mediaType) {
    const extensions = {
      audio: 'opus',
      image: 'jpg',
      video: 'mp4',
    };
    return extensions[mediaType] || 'bin';
  }
}

// Main integration class
class FingerprintWhatsAppIntegration {
  constructor(config) {
    this.converter = new FingerprintConverter(config);
    this.sender = new WhatsAppSender(config);
    this.config = config;
  }

  /**
   * Complete workflow: Convert → Push → Send → Cleanup
   */
  async sendMedia(s3Url, recipientJid, mediaType, options = {}) {
    const {
      afLevel = null,
      cleanupDelay = 5000,
    } = options;

    console.log(`\n${'='.repeat(60)}`);
    console.log(`📨 Sending ${mediaType} to ${recipientJid}`);
    console.log(`${'='.repeat(60)}\n`);

    try {
      // Step 1: Convert with anti-fingerprinting
      const converted = await this.converter.convertMedia(s3Url, mediaType, afLevel);

      // Step 2: Push to Android device
      const ext = this.sender.getExtension(mediaType);
      const remotePath = `${this.config.tempDir}/wa_${Date.now()}.${ext}`;
      await this.sender.pushToDevice(converted.processed_path, remotePath);

      // Step 3: Send via WhatsApp
      await this.sender.sendViaIntent(remotePath, recipientJid, mediaType);

      // Step 4: Cleanup (delayed)
      setTimeout(async () => {
        await this.sender.cleanupDevice(remotePath);
      }, cleanupDelay);

      console.log(`\n✅ Complete! Total time: ${converted.processing_time_ms}ms\n`);

      return {
        success: true,
        cache_hit: converted.cache_hit,
        size_increase: converted.size_increase_percent,
        processing_time: converted.processing_time_ms,
      };

    } catch (error) {
      console.error(`\n❌ Failed to send media: ${error.message}\n`);
      throw error;
    }
  }

  /**
   * Get cache statistics
   */
  async getStats() {
    return await this.converter.getCacheStats(this.config.deviceID);
  }

  /**
   * Health check
   */
  async healthCheck() {
    return await this.converter.checkHealth();
  }
}

// Export for use in your project
module.exports = {
  FingerprintConverter,
  WhatsAppSender,
  FingerprintWhatsAppIntegration,
};

// Example usage
if (require.main === module) {
  const integration = new FingerprintWhatsAppIntegration(CONFIG);

  // Example 1: Send audio
  integration.sendMedia(
    'https://s3.example.com/audio.mp3',
    '5511999999999@s.whatsapp.net',
    'audio'
  ).catch(console.error);

  // Example 2: Send image with paranoid level
  // integration.sendMedia(
  //   'https://s3.example.com/image.jpg',
  //   '5511999999999@s.whatsapp.net',
  //   'image',
  //   { afLevel: 'paranoid' }
  // ).catch(console.error);

  // Example 3: Get stats
  // integration.getStats().then(stats => {
  //   console.log('Cache Stats:', JSON.stringify(stats, null, 2));
  // });
}
