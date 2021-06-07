# temovex-exporter


```bash #deploy
# cross-compile for raspberry pi 3
GOOS=linux GOARCH=arm GOARM=7 go build -o temovex-exporter-linux-arm7

# deploy to rapsberry running ubuntu
ansible-playbook \
  --user ubuntu \
  --inventory "ventilation.localdomain," \
  playbook.yaml
```
