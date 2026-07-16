import requests

class KiwiClient:
    def __init__(self, server: str, token: str):
        self.server = server
        self.token = token

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
            response = requests.post(url, headers=headers, data=data, files=files)
            
        response.raise_for_status()
        return response.json()
