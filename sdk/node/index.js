const axios = require('axios');
const FormData = require('form-data');
const fs = require('fs');

class KiwiClient {
  constructor(server, token) {
    this.server = server;
    this.token = token;
  }

  async submitTask(task, file, testCmd, codebaseZipPath) {
    const formData = new FormData();
    formData.append('task', task);
    formData.append('file', file);
    formData.append('test_cmd', testCmd);
    formData.append('codebase', fs.createReadStream(codebaseZipPath));

    const response = await axios.post(`${this.server}/tasks`, formData, {
      headers: {
        'Authorization': `Bearer ${this.token}`,
        ...formData.getHeaders()
      }
    });
    return response.data;
  }
}

module.exports = { KiwiClient };
