$body = @{
   model = "llama-3.3-70b-versatile"
    messages = @(
        @{
            role = "user"
            content = "What is the capital of France?"
        }
    )
} | ConvertTo-Json -Depth 3

$headers = @{
    "Content-Type" = "application/json"
    "Authorization" = "gsk_YOUR_GROQ_KEY_HERE"
    "X-User-ID" = "user_1"
    "X-Feature-Name" = "chat"
}

$response = Invoke-RestMethod -Uri "http://localhost:8080/v1/chat/completions" -Method POST -Headers $headers -Body $body
$response | ConvertTo-Json -Depth 5
