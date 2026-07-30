CREATE TABLE choices IF NOT EXISTS (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    title VARCHAR(100),

    question_id UUID,
    FOREIGN KEY question_id REFERENCES questions(id)
)
