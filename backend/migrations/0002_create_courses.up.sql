CREATE TABLE courses IF NOT EXISTS (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    title VARCHAR(100),
    is_public BOOLEAN,

    user_id UUID,
    FOREIGN KEY user_id REFERENCES users(id)
)