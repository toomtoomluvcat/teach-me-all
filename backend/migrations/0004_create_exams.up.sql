CREATE TABLE exams IF NOT EXISTS (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    title VARCHAR(100),

    lesson_id UUID,
    FOREIGN KEY lesson_id REFERENCES lessons(id)
)