import sys, os, time, json, urllib.request, urllib.error

def test_gemini(api_key, model='gemini-2.5-flash'):
    url = 'https://generativelanguage.googleapis.com/v1beta/openai/chat/completions'
    headers = {
        'Content-Type': 'application/json',
        'Authorization': f'Bearer {api_key}'
    }
    payload = {
        'model': model,
        'messages': [{'role': 'user', 'content': 'Ответь кратко: тест связи'}],
        'max_tokens': 30
    }
    key_preview = api_key[:8] + '...' if len(api_key) > 8 else '***'
    print(f'🚀 Отправка прямого запроса в Google Gemini API...')
    print(f'   URL: {url}')
    print(f'   Модель: {model}')
    print(f'   Ключ: {key_preview}')
    
    t0 = time.time()
    req = urllib.request.Request(url, data=json.dumps(payload).encode('utf-8'), headers=headers)
    try:
        with urllib.request.urlopen(req, timeout=30) as resp:
            dur = time.time() - t0
            body = resp.read().decode('utf-8')
            data = json.loads(body)
            answer = data.get('choices', [{}])[0].get('message', {}).get('content', '')
            print(f'✅ УСПЕХ! Статус: {resp.status} (время: {dur:.2f}с)')
            print(f'   Ответ нейросети: {answer.strip()}')
            return True
    except urllib.error.HTTPError as e:
        dur = time.time() - t0
        err_body = e.read().decode('utf-8')
        print(f'❌ ОШИБКА HTTP {e.code} (время: {dur:.2f}с)')
        print(f'   Детали: {err_body}')
        return False
    except Exception as e:
        print(f'❌ Сбой сети/соединения: {e}')
        return False

if __name__ == '__main__':
    key = sys.argv[1] if len(sys.argv) > 1 else os.getenv('GOOGLE_API_KEY')
    model = sys.argv[2] if len(sys.argv) > 2 else 'gemini-2.5-flash'
    if not key:
        print('Использование: python3 test_direct.py <GOOGLE_API_KEY> [model]')
        sys.exit(1)
    test_gemini(key, model)
