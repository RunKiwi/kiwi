import json
import subprocess

print("Fetching issues...")
output = subprocess.check_output(['gh', 'issue', 'list', '--state', 'open', '--limit', '100', '--json', 'number,title,body'])
issues = json.loads(output)

for issue in issues:
    number = issue['number']
    title = issue['title']
    body = issue['body']
    
    new_body = body.replace(' (Belongs to EPIC #)', '')
    new_body = new_body.replace('Belongs to #', '')
    if new_body.strip() == '':
        new_body = 'See milestone for details.'
        
    subprocess.run(['gh', 'issue', 'edit', str(number), '--body', new_body.strip()])
    
    if title.startswith('Phase P') or title.startswith('[EPIC]'):
        subprocess.run(['gh', 'issue', 'close', str(number), '-r', 'not planned', '-c', 'Converted to GitHub Milestone.'])
        print(f"Closed {number}: {title}")
        continue
        
    milestone = None
    if title.startswith('P1.'): milestone = "Phase P1: Control-plane foundation"
    elif title.startswith('P2.'): milestone = "Phase P2: Master/Worker agents + checkpointing"
    elif title.startswith('P3.'): milestone = "Phase P3: Security & trust boundary hardening"
    elif title.startswith('P4.'): milestone = "Phase P4: Observability & scale"
    elif title.startswith('P5.'): milestone = "Phase P5: Governance & extensibility"
    elif title.startswith('D'): milestone = "Phase P6: Deployment Strategy & BYOC Runner"
    
    if milestone:
        subprocess.run(['gh', 'issue', 'edit', str(number), '-m', milestone])
        print(f"Assigned {number} to {milestone}")

print("Done fixing issues!")
