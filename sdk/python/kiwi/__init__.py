import requests

class KiwiClient:
    def __init__(self, server: str, token: str = None):
        if server.startswith("http://") and "localhost" not in server and "127.0.0.1" not in server:
            raise ValueError("Refusing to send token over cleartext HTTP. Use HTTPS.")
        import os
        self.server = server
        self.token = token or os.environ.get("KIWI_SERVER_TOKEN")

    def submit_task(self, task: str, file: str, test_cmd: str, codebase_zip_path: str):
        url = f"{self.server}/tasks"
        headers = {"Authorization": f"Bearer {self.token}"}
        data = {
            "task": task,
            "file": file,
            "test_cmd": test_cmd
        }
        
        with open(codebase_zip_path, "rb") as f:
            files = {"codebase": f}
            response = requests.post(url, headers=headers, data=data, files=files, timeout=10.0)
            
        response.raise_for_status()
        return response.json()
