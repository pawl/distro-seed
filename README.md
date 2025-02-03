# Distro Seed

Distro Seed is a **lightweight Go-based BitTorrent seeder** designed for **low-resource VPS hosting**.

After trying **qBittorrent, rTorrent, and Transmission (via Docker)** on a **1GB RAM VPS**, I ran into **OOM issues and configuration headaches**. Instead of tweaking settings endlessly, I built this small **Go program** for a **simpler, more efficient deployment**.

This project provides an **Ansible playbook** to set up a **fresh Ubuntu VPS** for automatic Linux torrent seeding.

---

## **🛠 Installation**
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

## **⚙️ Configuration**
```bash
cp hosts.example hosts
cp vars.example.yml vars.yml
```
- **Edit `hosts`**: Add your server's IP
- **Edit `vars.yml`**: Configure SSH key, seeder user, and torrent sources

### **SSH Configuration Assumptions**
- You must specify your **public SSH key** in `vars.yml`, e.g.:
  ```yaml
  public_key_path: "~/.ssh/<your key>.pub"
  ```
- This **disables SSH password authentication** for security, which some low-end VPS providers **don't disable by default**.

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

To check upload stats:
```bash
ssh root@<your-server-ip>
cat /opt/distro-seed/downloads/seed_stats.txt
```

To monitor logs:
```bash
journalctl -u distro-seed -f
```
