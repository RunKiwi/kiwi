const axios = require('axios');
const FormData = require('form-data');
const fs = require('fs');

class KiwiClient {
  constructor(server, token) {
    if (server.startsWith('http://') && !server.includes('localhost') && !server.includes('127.0.0.1')) {
      throw new Error('Refusing to send token over cleartext HTTP to remote server. Use HTTPS.');
    }
    this.server = server;
    this.token = token || process.env.KIWI_SERVER_TOKEN;
  }

  async submitTask(task, file, testCmd, codebaseZipPath) {
    const formData = new FormData();
    formData.append('task', task);
    formData.append('file', file);
    formData.append('test_cmd', testCmd);
    formData.append('codebase', fs.createReadStream(codebaseZipPath));

    try {
      const response = await axios.post(`${this.server}/tasks`, formData, {
        timeout: 10000,
        headers: {
          'Authorization': `Bearer ${this.token}`,
          ...formData.getHeaders()
        }
      });
      return response.data;
    } catch (error) {
      if (error.config && error.config.headers) {
        delete error.config.headers.Authorization;
      }
      throw error;
    }
  }
}

module.exports = { KiwiClient };
