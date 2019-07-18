import React, { Component } from 'react'
import styled from 'styled-components'
import { Link } from 'react-router-dom'

const Container = styled.div`
  height: 100%;
  /* display: grid;
  grid-template-columns: 80px 1fr 500px; */
`

const LeftSidebar = styled.div`
  height: 100%;
  width: 80px;
  position: fixed;
  top: 60px;
  left: 0;
  padding-top: 10px;
`

const RightSidebar = styled.div`
  height: 100%;
  width: 500px;
  position: fixed;
  right: 0;
  top: 60px;
  background-color: white;
`

const Content = styled.div`
  margin-top: 60px;
  margin-left: 80px;
  margin-right: 500px;
  padding: 12px 8px 0;
`

const SideButton = styled(Link)`
  border: 1px solid #eee;
  text-align: center;
  padding-top: 17px;
  border-radius: 50%;
  display: block;
  width: 60px;
  height: 60px;
  margin: 10px;
`

const Header = styled.div`
  height: 60px;
  width: 100%;
  position: fixed;
  background: white;
  top: 0;
  /* border-bottom: 1px solid rgba(0, 0, 0, 0.1); */
  box-shadow: 0 0 2px rgba(0, 0, 0, 0.3);
`

const Title = styled.h1`
  font-size: 36px;
  padding: 5px 12px;
`

class Layout extends Component {
  render() {
    return (
      <Container>
        <LeftSidebar>
          <SideButton to="/">Albums</SideButton>
        </LeftSidebar>
        <Content>{this.props.children}</Content>
        <RightSidebar>Right</RightSidebar>
        <Header>
          <Title>Photoview</Title>
        </Header>
      </Container>
    )
  }
}

export default Layout