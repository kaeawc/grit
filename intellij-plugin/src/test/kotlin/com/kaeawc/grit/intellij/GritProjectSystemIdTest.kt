package com.kaeawc.grit.intellij

import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertNotNull
import org.junit.jupiter.api.Test

class GritProjectSystemIdTest {

    @Test
    fun `system ID has expected string value`() {
        assertEquals("GRIT", GritProjectSystemId.GRIT.id)
    }

    @Test
    fun `system ID is not null`() {
        assertNotNull(GritProjectSystemId.GRIT)
    }

    @Test
    fun `system ID readable name defaults to id`() {
        assertEquals("Grit", GritProjectSystemId.GRIT.readableName)
    }
}
