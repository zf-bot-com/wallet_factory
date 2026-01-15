# Tron Vanity Address Generator - User Guide

## Quick Start

### 1. Extract Files

Extract the archive to any directory:

```bash
# Linux
tar -xzvf trap-factory-linux.tar.gz

# Windows
# Use extraction tool to extract trap-factory-win.zip
```

### 2. Add Execute Permission (Linux)

```bash
chmod +x factory-linux profanity.x64
```

### 3. View Help

```bash
# Linux
./factory-linux help
# or
./factory-linux -h
# or
./factory-linux --help

# Windows
factory-win.exe help
```

## File Description

The package contains:

- **factory-linux** / **factory-win.exe** - Main program
- **profanity.x64** / **profanity.exe** - GPU acceleration tool (required)
- **profanity.txt** - Pattern matching file (required, for 5a/6a/7a/8a tasks)
- **USER_GUIDE.md** - This file

## Usage

The program supports two modes:

### Mode 1: build Mode (Direct Generation)

Generate vanity addresses directly from command line.

**Basic Format:**
```
<executable> build <target_address> <prefix_count> <suffix_count> <quit_count>
```

**Parameters:**
- `target_address`: Target address template to match
- `prefix_count`: Number of prefix characters to match (e.g., `1` means first character must be T)
- `suffix_count`: Number of suffix characters to match (e.g., `3` means last 3 characters must be 888)
- `quit_count`: Number of matching addresses to generate before exiting

**Examples:**

```bash
# Linux Examples
# Generate address with first char T, last 3 chars 888, generate 1 address
./factory-linux build TTTCqtavqZiKEMVYgEQSN2b91h88888888 1 3 1

# Generate address with first char T, last 4 chars 6666, generate 5 addresses
./factory-linux build TTTCqtavqZiKEMVYgEQSN2b91h66666666 1 4 5

# Windows Examples
.\factory-win.exe build TTTCqtavqZiKEMVYgEQSN2b91h88888888 1 3 1
.\factory-win.exe build TTTCqtavqZiKEMVYgEQSN2b91h66666666 1 4 5
```

**Output Format:**
```
<private_key> <address> (生成数量: <total_generated>)
```

**Example Output:**
```
60641bd35aaf9df6472107f8cc11dce1f91f64c545177cff4be504ba064cbb7b TGNrA1SQfL2SPLGzsnyaWYHDiSCHXS888z (生成数量: 124048000)
```

### Mode 2: server Mode (Server Mode)

Run as a server, fetching tasks from Redis queue and processing them.

**Start Server:**

```bash
# Linux
./factory-linux server

# Windows
factory-win.exe server
```

**How It Works:**

1. Program connects to Redis server (configuration is built-in)
2. Listens to input queue `address_producer` for tasks
3. Processes tasks and generates vanity addresses
4. Pushes results to output queue `address_consumer`

**Task Format (JSON):**

Send tasks to Redis queue `address_producer`:

```json
{
  "taskId": "task_id",
  "taskType": "task_type",
  "customFormat": "custom_format (optional)"
}
```

**Supported Task Types:**

- **`5a`, `6a`, `7a`, `8a`**: Generate addresses with last 5/6/7/8 identical characters
  - Example: `{"taskId": "123", "taskType": "5a"}`
  
- **`custom_address`**: Custom format
  - Requires `customFormat` field, format: `prefix-suffix`
  - Example: `{"taskId": "123", "taskType": "custom_address", "customFormat": "TABC-8888"}`

**Result Format (JSON):**

Results are pushed to Redis queue `address_consumer`:

```json
{
  "taskId": "task_id",
  "status": "completed",
  "result": {
    "privateKey": "private_key",
    "address": "address",
    "totalGenerated": total_generated_count
  }
}
```

If task fails:

```json
{
  "taskId": "task_id",
  "status": "failed"
}
```

## System Requirements

### Linux

- GPU with OpenCL support (NVIDIA/AMD/Intel)
- OpenCL drivers installed
- Ensure `profanity.x64` has execute permission

### Windows

- GPU with OpenCL support (NVIDIA/AMD/Intel)
- Graphics drivers installed
- May require Visual Studio runtime libraries

### Verify GPU Environment (Linux)

```bash
# Install clinfo (if not installed)
sudo apt install -y clinfo  # Ubuntu/Debian

# Verify GPU
clinfo | grep -i "device name"
```

If you can see GPU information (e.g., `NVIDIA GeForce RTX 4090`), the environment is correctly configured.

## Troubleshooting

### 1. "Executable file not found" Error

**Problem:** Program cannot find `profanity.x64` or `profanity.exe`

**Solution:** Ensure `profanity.x64` (Linux) or `profanity.exe` (Windows) is in the same directory as the main program

### 2. "Pattern file not found" Error

**Problem:** Program cannot find `profanity.txt` file

**Solution:** Ensure `profanity.txt` file is in the same directory as the main program

### 3. GPU Not Working

**Problem:** Program runs slowly or reports errors

**Solution:**
- Check if GPU drivers are installed
- Verify OpenCL environment: run `clinfo` to check GPU information
- Ensure GPU supports OpenCL

### 4. Redis Connection Failed (server mode)

**Problem:** Connection failure when starting server mode

**Solution:**
- Check if Redis server is accessible
- Check network connection
- Redis configuration is built into the program, contact developer if modification is needed

## Important Notes

1. **Private Key Security**: Keep generated private keys secure, do not share with others
2. **Address Verification**: Verify the correspondence between private key and address before using
3. **Multi-signature Protection**: Enable multi-signature for generated addresses to improve security
4. **File Integrity**: Ensure all required files are in the same directory
5. **Task Timeout**: In server mode, single task processing timeout is 30 minutes

## Technical Support

If you encounter problems, please provide:
- Operating system version
- GPU model
- Error messages
- Command used

---

**Version Info:** Check program help for version number
