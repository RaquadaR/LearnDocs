package com.example;
public class MyApp {
    public String getMessage() {
        return "Hello, world!";
    }
}
```src/test/java/com/example/MyAppTest.java`:

```java
package com.example;
import org.junit.Test;
import static org.junit.Assert.assertEquals;
public class MyAppTest {
    @Test
    public void testGetMessage() {
        MyApp app = new MyApp();
        assertEquals("Hello, world!", app.getMessage());
    }
}