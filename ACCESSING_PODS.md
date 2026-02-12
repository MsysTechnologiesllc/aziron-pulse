# Accessing Pods via Aziron UI

## Architecture Overview

```
Aziron UI (localhost:3000)
    ↓
Aziron Server (localhost:8080)
    ↓ /api/v1/pulse/*
Aziron Pulse (localhost:8081)
    ↓ /pulse/{pulse_id}/*
Code-Server Pod (NodePort or ClusterIP)
```

## Access Methods

### Method 1: Direct Access (NodePort)

Your provisioned pod has a NodePort service that exposes code-server directly:

```bash
# Get the NodePort
kubectl get svc pulse-f1a8c99c-svc -n pulse-tenant-37a8eec1ce19687d

# Access directly via minikube IP
open http://192.168.49.2:31236
```

**Pod Details:**
- Pulse ID: `f1a8c99c-e024-4877-9375-0268ab025f22`
- NodePort: `31236`
- Minikube IP: `192.168.49.2`
- Direct URL: **http://192.168.49.2:31236**

### Method 2: Via Aziron Server Proxy (Recommended)

Access through the aziron-server proxy which handles authentication:

```bash
# Using the JWT token
curl -H "Authorization: Bearer <YOUR_JWT_TOKEN>" \
  http://localhost:8080/api/v1/pulse/pulse/f1a8c99c-e024-4877-9375-0268ab025f22/
```

**Proxy URL Structure:**
```
http://localhost:8080/api/v1/pulse/pulse/{pulse_id}/*
```

### Method 3: Via Aziron UI (Frontend)

The Aziron UI can embed the code-server using an iframe or redirect:

#### Option A: Iframe Embed

Add a component in your UI to display the code-server:

```jsx
// In aziron-ui/src/components/PulseIDE.jsx
import { useEffect, useState } from 'react';

export function PulseIDE({ pulseId, token }) {
  const [iframeUrl, setIframeUrl] = useState('');

  useEffect(() => {
    // Use the proxy URL through aziron-server
    const backendUrl = import.meta.env.VITE_BACKEND_BASE_URL || 'http://localhost:8080';
    const url = `${backendUrl}/api/v1/pulse/pulse/${pulseId}/?token=${token}`;
    setIframeUrl(url);
  }, [pulseId, token]);

  return (
    <div className="w-full h-screen">
      <iframe
        src={iframeUrl}
        className="w-full h-full border-0"
        title="Code Server"
        allow="clipboard-read; clipboard-write"
      />
    </div>
  );
}
```

#### Option B: Direct Navigation

Navigate user directly to the proxied URL:

```javascript
// Get pulse ID from provision response
const pulseId = 'f1a8c99c-e024-4877-9375-0268ab025f22';
const token = localStorage.getItem('authToken');

// Redirect to code-server via proxy
window.location.href = `http://localhost:8080/api/v1/pulse/pulse/${pulseId}/?token=${token}`;
```

## Complete Flow Example

### 1. Provision Pod from UI

```javascript
// In your React component
const provisionPod = async () => {
  const token = localStorage.getItem('authToken');
  const response = await fetch('http://localhost:8080/api/v1/pulse/provision', {
    method: 'POST',
    headers: {
      'Authorization': `Bearer ${token}`,
      'Content-Type': 'application/json'
    },
    body: JSON.stringify({
      base_image: 'codercom/code-server:latest',
      cpu_limit: 2.0,
      memory_mb: 4096,
      storage_gb: 10,
      metadata: {
        user_email: 'admin@example.com',
        tenant_id: 'test-tenant',
        instance_tier: 'balanced'
      }
    })
  });

  const data = await response.json();
  return data.pulse_id; // e.g., 'f1a8c99c-e024-4877-9375-0268ab025f22'
};
```

### 2. Open Code-Server in New Window

```javascript
const openCodeServer = (pulseId) => {
  const token = localStorage.getItem('authToken');
  const url = `http://localhost:8080/api/v1/pulse/pulse/${pulseId}/?token=${token}`;
  
  window.open(url, '_blank', 'width=1920,height=1080');
};
```

### 3. Embed in Dashboard

```javascript
// Add to your dashboard component
import { PulseIDE } from '@/components/PulseIDE';

function Dashboard() {
  const [activePulseId, setActivePulseId] = useState(null);
  const token = localStorage.getItem('authToken');

  return (
    <div className="flex h-screen">
      <Sidebar onSelectPulse={setActivePulseId} />
      
      {activePulseId && (
        <PulseIDE pulseId={activePulseId} token={token} />
      )}
    </div>
  );
}
```

## Current Active Pod

Your currently running pod:

- **Pulse ID**: `f1a8c99c-e024-4877-9375-0268ab025f22`
- **Pod Name**: `pulse-f1a8c99c`
- **Namespace**: `pulse-tenant-37a8eec1ce19687d`
- **Status**: Running ✅
- **Node Port**: `31236`
- **TTL**: 120 minutes (expires at 23:20:11 IST)

### Quick Access URLs

**Direct Access (NodePort):**
```
http://192.168.49.2:31236
```

**Via Aziron Server Proxy:**
```
http://localhost:8080/api/v1/pulse/pulse/f1a8c99c-e024-4877-9375-0268ab025f22/
```

**With Authentication Header:**
```bash
curl -H "Authorization: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJ1c2VyX2lkIjoiMDAwMDAwMDAtMDAwMC0wMDAwLTAwMDAtMDAwMDAwMDAwMDAxIiwidXNlcm5hbWUiOiJhZG1pbiIsInJvbGUiOiJhZG1pbiIsImlzcyI6ImF6aXJvbiIsInN1YiI6IjAwMDAwMDAwLTAwMDAtMDAwMC0wMDAwLTAwMDAwMDAwMDAwMSIsImV4cCI6MTc3MDk2ODY3NSwibmJmIjoxNzcwODgyMjc1LCJpYXQiOjE3NzA4ODIyNzV9.GF9ihvwOgAf50lw18Rj-nUlFTaFhjLLVlpTbDtxvRms" \
  http://localhost:8080/api/v1/pulse/pulse/f1a8c99c-e024-4877-9375-0268ab025f22/
```

## Troubleshooting

### Issue: Can't access via proxy

**Solution**: Verify aziron-server proxy is registered:
```bash
curl -I http://localhost:8080/api/v1/pulse/health
```

### Issue: NodePort not accessible

**Solution**: Check minikube tunnel or use port-forward:
```bash
# Option 1: Minikube tunnel (requires sudo)
minikube tunnel

# Option 2: Port forward
kubectl port-forward -n pulse-tenant-37a8eec1ce19687d svc/pulse-f1a8c99c-svc 8888:8080
# Then access at http://localhost:8888
```

### Issue: Code-server requires password

**Solution**: Code-server is configured without password in the pod. If prompted, check pod logs:
```bash
kubectl logs pulse-f1a8c99c -n pulse-tenant-37a8eec1ce19687d
```

## API Endpoints

### List All Pods (GET)
```bash
curl -H "Authorization: Bearer <TOKEN>" \
  http://localhost:8080/api/v1/pulse/provision
```

### Get Specific Pod (GET)
```bash
curl -H "Authorization: Bearer <TOKEN>" \
  http://localhost:8080/api/v1/pulse/provision/f1a8c99c-e024-4877-9375-0268ab025f22
```

### Delete Pod (DELETE)
```bash
curl -X DELETE \
  -H "Authorization: Bearer <TOKEN>" \
  http://localhost:8080/api/v1/pulse/provision/f1a8c99c-e024-4877-9375-0268ab025f22
```

### Health Check (GET)
```bash
curl -H "Authorization: Bearer <TOKEN>" \
  http://localhost:8080/api/v1/pulse/pulse/f1a8c99c-e024-4877-9375-0268ab025f22/health
```

## Next Steps

1. **Update UI**: Add pulse IDE component to aziron-ui
2. **User Dashboard**: Show list of active pods per user
3. **Auto-provision**: Provision pod automatically on first login
4. **TTL Management**: Add UI to extend TTL before expiration
5. **Metrics Dashboard**: Display resource usage from Prometheus metrics
