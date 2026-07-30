CREATE TABLE lessons IF NOT EXISTS (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    title VARCHAR(100),
    
    course_id UUID,
    FOREIGN KEY course_id REFERENCES courses(id)
)