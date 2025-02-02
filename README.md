# Distro Seed

Distro Seed is a **lightweight Go-based BitTorrent seeder** designed for **low-resource VPS hosting**. It automates **fetching, seeding, and managing community Linux torrents** using Ansible.

---

## **🛠 Installation**
### **1. Install Dependencies on Mac**
```bash
# Install asdf (https://asdf-vm.com/guide/getting-started.html)

# Install dependencies
asdf plugin add python && asdf install python 3.12.8 && asdf global python 3.12.8
asdf plugin add rust && asdf install rust latest && asdf global rust latest
brew install openssl

# Set up virtual environment
python -m venv .venv
source .venv/bin/activate

# Install Ansible & dependencies
pip install --upgrade pip
pip install -r requirements.txt
ansible-galaxy install -r requirements.yml
ansible-galaxy collection install -r requirements.yml
```

---

## **⚙️ Precompile the Go Binary on macOS**
Before deploying, compile the Go binary **for Linux**:
```bash
GOOS=linux GOARCH=amd64 go build -o distro-seed-linux main.go
```
This will generate a **Linux-compatible binary**.

---

## **🚀 Running the Seeder Locally (For Testing)**
To test on macOS:
```bash
go run main.go -dir ./downloads -url "https://cdimage.debian.org/debian-cd/current/amd64/bt-cd/debian-12.9.0-amd64-netinst.iso.torrent"
```
To monitor logs:
```bash
tail -f downloads/seed_stats.txt
```

---

## **📡 Deploying with Ansible**
```bash
ansible-playbook -i hosts ansible-playbook.yml
```

To check service status:
```bash
ssh root@<your-server-ip>
systemctl status distro-seed
```

To monitor logs:
```bash
journalctl -u distro-seed -f
```
