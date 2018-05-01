from invoke import task
import requests

@task
def update(ctx):
    r = requests.get('https://mkcert.org/generate/')
    r.raise_for_status()
    certs = r.content

    with open('certifi.go', 'rb') as f:
        file = f.read()

    file = file.split('`\n')
    assert len(file) == 3
    file[1] = certs

    ctx.run("rm certifi.go")

    with open('certifi.go', 'wb') as f:
        f.write('`\n'.join(file))
