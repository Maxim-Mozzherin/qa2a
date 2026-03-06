-- 1. Компании (Заведения)
CREATE TABLE companies (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    created_at TIMESTAMP DEFAULT NOW()
);

-- 2. Юзеры
CREATE TABLE users (
    id SERIAL PRIMARY KEY,
    tg_id BIGINT UNIQUE NOT NULL,
    username VARCHAR(255),
    full_name VARCHAR(255),
    created_at TIMESTAMP DEFAULT NOW()
);

-- 3. Членство (связка юзер + компания + роль)
CREATE TABLE memberships (
    user_id INTEGER REFERENCES users(id) ON DELETE CASCADE,
    company_id INTEGER REFERENCES companies(id) ON DELETE CASCADE,
    role VARCHAR(50) DEFAULT 'user', -- owner, manager, admin, user
    PRIMARY KEY (user_id, company_id)
);

-- 4. Операции
CREATE TABLE operations (
    id SERIAL PRIMARY KEY,
    company_id INTEGER REFERENCES companies(id),
    user_id INTEGER REFERENCES users(id),
    type VARCHAR(50) NOT NULL, -- 'writeoff', 'transfer', 'procurement'
    position_name VARCHAR(255) NOT NULL,
    quantity NUMERIC(10,2) NOT NULL,
    unit VARCHAR(20) NOT NULL,
    status VARCHAR(20) DEFAULT 'pending', -- pending, approved, rejected
    created_at TIMESTAMP DEFAULT NOW()
);

-- 5. Остатки (Кэш)
CREATE TABLE balances (
    company_id INTEGER REFERENCES companies(id),
    position_name VARCHAR(255) NOT NULL,
    quantity NUMERIC(10,2) DEFAULT 0,
    unit VARCHAR(20) NOT NULL,
    PRIMARY KEY (company_id, position_name)
);
